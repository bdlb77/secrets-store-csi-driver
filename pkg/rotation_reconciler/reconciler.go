// create/update objectVersions with mounted file content
// add versions info to secretProviderClass obj's versions status field

package rotation_reconciler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	corev1 "k8s.io/api/core/v1"

	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"

	"github.com/prometheus/common/log"
	"sigs.k8s.io/secrets-store-csi-driver/apis/v1alpha1"
)

type Parameters struct {
	KeyVaultName           string `json:"keyvaultName" yaml:"keyvaultName"`
	TenantID               string `json:"tenantId" yaml:"tenantId"`
	Objects                string `json:"objects" yaml:"objects"`
	UsePodIdentity         string `json:"usePodIdentity" yaml:"usePodIdentity"`
	UseVMManagedIdentity   string `json:"useVMManagedIdentity" yaml:"useVMManagedIdentity"`
	UserAssignedIdentityID string `json:"userAssignedIdentityId" yaml:"userAssignedIdentityId"`
}
type Payload struct {
	Provider       string `json:"provider" yaml:"provider"`
	TargetPath     string
	Parameters     Parameters `json:"parameters" yaml:"parameters"`
	Secrets        Secrets
	ProviderName   string            `json:"name" yaml:"name"`
	CurrentObjects []map[string]string `json:"currentObjects" yaml:"currentObjects"`
}
type Secrets struct {
	ClientID     string `json:"clientid" yaml:"clientid"`
	ClientSecret string `json:"clientsecret" yaml:"clientsecret"`
}

const (
	parametersField    = "paramaters"
	providerName       = "azure"
	providerVolumePath = "/etc/kubernetes/secrets-store-csi-providers"
)

// Reconciler is main functionality
func Reconciler(ctx context.Context) {
	/*
		1. Get List of SPC Bindings
		2. Foreach binding
			3.  targetPath from binding
			4. query SPC
				5. Get Objects, Keyvault Name, tenantId, podIdentity? (attributes)
			6. Query pod (name is status)
				7. get nodePublishSecret Ref
				8. Make call to K8s secrets using name of K8s secret from NodePublishSecretRef
				9. Append K8s secrets to Payload.Secrets
			10. Fetch provider binary
	*/
	spcBindings, err := getSecretProviderClassPodStatuses(ctx)
	if err != nil {
		log.Errorf("Failed to Receive SecretProviderClassPodStatuses from Cluster, Err: %v", err)
		return
	}

	for i, spcBinding := range spcBindings.Items {
		var payload Payload
		c, err := secretsstore.GetClient()
		if err != nil {
			log.Errorf("Failed to create Client, Err: %v", err)
			return
		}
		namespace := spcBinding.GetNamespace()
		payload.TargetPath, err = secretsstore.getStringFromObjectSpec(spcBinding.Object, "targetPath")
		podName, err := secretsstore.getStringFromObjectSpec(spcBinding.Object, "podName")
		spcName, err := secretsstore.getStringFromObjectSpec(spcBinding.Object, "secretProviderClassName")
		// fetch SecretProviderClass from K8s
		var spc v1alpha1.SecretProviderClass
		err = getSPC(ctx, namespace, spcName, &spc)
		if err != nil {
			log.Errorf("failed to get spc %s, err: %+v", spcName, err)
		}
		// fetch Pod from K8s API
		var pod corev1.Pod
		err = getPod(ctx, namespace, podName, &pod)
		payload.Provider = spc.Spec.Provider
		parameters := spc.Spec.Parameters
		// add parameters to payload
		json.Unmarshal([]byte(parameters), &payload.Parameters)
		// add currentObjects to payload
		inClusterObjects := spc.Status.Objects
		payload.CurrentObjects, err := json.Marshal(inClusterObjects)
		if err != nil {
			log.Errorf("Failed to marshal currentObjects in Cluster for Payload for spc: %s, Err: %v", spcName, err)
		}
		usePodIdentity := payload.Parameters.UsePodIdentity
		if inClusterObjects == nil {
			log.Infof("No Secret Objects present in Cluster for spc: %v", spcName)
		}
		if usePodIdentity == "false" {
			secretRefName := getNodePublishSecretRefFromPod(ctx, pod)
			if secretRefName == "" || secretRefName == nil {
				log.Errorf("Could not find nodePublishSecretRef from pod: %s. Err: %v", pod.GetName(), err)
				return
			}
		}

		// add secrets to payload
		err = buildK8SecretsPayload(ctx, secretRefName, namespace, &payload)
		if err != nil {
			log.Errof("Failed to Add Secrets to Payload for spc: %s, err: %v", spcName, err)
		}
		spew.Dump(payload)
		// err = fetchProviderBinary(payload)
		// if err != nil {
		// 	log.Errorf("Failed to Call Provider Binary. Err: %v", err)
		// 	return
		// }
		/*
			1. get both ObjStr from config'd objects & running Objects.. Pass both to provider.
				- Provider will send config'd objects to KeyVault, and use RunningObjects to compare versions.


		*/

	}
}

func fetchProviderBinary(payload Payload) error {
	providerBinary := fmt.Sprintf("%s/%s/provider-%s", providerVolumePath, payload.Provider, payload.Provider)
	parametersStr, err := json.Marshal(payload.Paramters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters, Err: %v", err)
	}
	currentObjsStr, err := json.Marshal(payload.CurrentObjects)
	if err != nil {
		return fmt.Errorf("failed to marshal current Objects, Err: %v", err)
	}
	secretStr, err := json.Marshal(payload.Secrets)
	if err != nil {
		log.Errorf("failed to marshal secrets")
		return fmt.Errorf("failed to marshal Secrets, Err: %v", err)
	}

	args := []string{
		"--attributes", string(parametersStr),
		"--secrets", string(secretStr),
		"--targetPath", payload.TargetPath,
		"--permission", "420",
	}
	log.Infof("provider command invoked: %s %s %v", providerBinary,
		"--attributes [REDACTED] --secrets [REDACTED]", args[4:])
	cmd := exec.Command(
		providerBinary,
		args...,
	)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stderr, cmd.Stdout = stderr, stdout

	err = cmd.Run()

	if err != nil {
		log.Errorf("err: %v, output: %v", err, stderr.String())
		// log.Errorf("error invoking provider, err: %v, output: %v", err, stderr.String())
	}
	return nil
}

func buildK8sSecretsPayload(ctx context.Context, secretsName, namespace string, payload *Payload) error {
	k8sSecrets := make(map[string]string)
	secret, err := secretsstore.GetK8sSecret(ctx, secretsName, namespace)
	if err != nil {
		return err
	}
	for k, v := range secret.Data {
		k8sSecrets[k] = string(v)
	}
	k8sSecretsJSON, err := json.Marshal(k8sSecrets)

	if err != nil {
		return err
	}
	_ = json.Unmarshal(k8sSecretsJSON, &payload.Secrets)

	return nil
}

func getNodePublishSecretRefFromPod(ctx context.Context, pod corev1.Pod) string {
	var secretsName string
	for _, vol := range pod.Spec.Volumes {
		if vol.CSI == nil || vol.CSI.Driver != "secrets-store.csi.k8s.io" {
			continue
		}
		// TODO: check type of driver
		secretsName = vol.CSI.NodePublishSecretRef.Name
	}
	return string, nil
}

// func podInfoToPayload(ctx context.Context, payload *Payload, obj map[string]interface{}, spcName string, usePodIdentity string) error {
// 	log.Infof("Fetching Pods From Status Field in SPC: %v", spcName)
// 	statusPods, exists, err := unstructured.NestedSlice(obj, "status", "byPod")
// 	if err != nil {
// 		return fmt.Errorf("Issue parsing byPod information in the Status Field for Secret Provider Class: %v, Error: %v", spcName, err)
// 	}
// 	if !exists {
// 		return fmt.Errorf("No pods tracked in Status Field for Secret Provider Class: %v, Error: %v", spcName, err)
// 	}

// 	if err != nil {
// 		return fmt.Errorf("Issue slicing params from SPC: %v, Error: %v", spcName, err)
// 	}
// 	for _, p := range statusPods {
// 		podMap, _ := p.(map[string]interface{})
// 		name, namespace := podMap["name"].(string), podMap["namespace"].(string)

// 		podKey := types.NamespacedName{
// 			Namespace: namespace,
// 			Name:      name,
// 		}
// 		pod := &corev1.Pod{}
// 		client, err := secretsstore.GetClient()
// 		if err != nil {
// 			return fmt.Errorf("issue with creating client, Error: %v", err)
// 		}
// 		err = client.Get(ctx, podKey, pod)
// 		if err != nil {
// 			return fmt.Errorf("Issue Fetching Pod from API, Error: %v", err)
// 		}
// 		podUID := pod.UID
// 		var secretsName, mountName string
// 		for _, vol := range pod.Spec.Volumes {
// 			if vol.CSI == nil {
// 				continue
// 			}
// 			// TODO: check type of driver
// 			if usePodIdentity == "false" {
// 				secretsName = vol.CSI.NodePublishSecretRef.Name
// 			}
// 			mountName = vol.Name
// 		}
// 		payload.TargetPath = fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes.io~csi/%s/mount", podUID, mountName)

// 		log.Infof("Adding K8s Secrets to Payload for SPC: %v", spcName)
// 		if usePodIdentity == "false" {
// 			err = addK8sSecretsToPayload(ctx, secretsName, namespace, payload)
// 			if err != nil {
// 				return fmt.Errorf("Issue forming K8s secrets inside payload Struct. error: %v", err)
// 			}
// 		}
// 	}
// 	return nil
// }
