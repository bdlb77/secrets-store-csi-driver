// create/update objectVersions with mounted file content
// add versions info to secretProviderClass obj's versions status field

package rotationreconciler

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"

	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/common/log"
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
	ProviderName   string              `json:"name" yaml:"name"`
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

	if spcBindings == nil {
		log.Errorf("No SPC Bindings present in cluster.")
		return
	}
	for _, spcBinding := range spcBindings.Items {
		var payload Payload
		if err != nil {
			log.Errorf("Failed to create Client, Err: %v", err)
			return
		}
		namespace := spcBinding.GetNamespace()
		payload.TargetPath, err = secretsstore.GetStringFromObjectStatus(spcBinding.Object, "targetPath")
		podName, err := secretsstore.GetStringFromObjectStatus(spcBinding.Object, "podName")
		spcName, err := secretsstore.GetStringFromObjectStatus(spcBinding.Object, "secretProviderClassName")
		// fetch SecretProviderClass from K8s
		// spc := &v1alpha1.SecretProviderClass{}

		spc, err := getSPC(context.TODO(), namespace, spcName)
		if err != nil {
			log.Errorf("failed to get spc %s, err: %+v", spcName, err)
		}

		// add currentObjects to payload
		err = buildRunningObjectsInPayload(&spcBinding, &payload)
		if err != nil {
			log.Errorf("Failed to Set Current Objects into Payload, Err: %v", err)
			continue
		}
		// fetch Pod from K8s API
		var pod corev1.Pod
		err = getPod(context.TODO(), namespace, podName, &pod)
		if err != nil {
			log.Errorf("Failed to get pod: %s, err: %v", podName, err)
			continue
		}
		// set provider to payload
		err = setProviderTypeInPayload(spc.Object, &payload)
		if err != nil {
			log.Errorf("Failed to get Provider Type From SPC: %s, Err: %v", spc.GetName(), err)
			continue
		}

		params, err := secretsstore.GetMapFromObjectSpec(spc.Object, "parameters")
		if err != nil {
			log.Errorf("Failed to get Parameters from SPC: %s, Err: %v", spc.GetName(), err)
			continue
		}
		// add parameters to payload
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			log.Errorf("Failed to convert paramteres from spc to JSON, err: %v", err)
			continue
		}
		err = json.Unmarshal(paramsJSON, &payload.Parameters)
		if err != nil {
			log.Errorf("Issue Unmarshaling params from instance of SPC: %v, Error: %v ", spcName, err)
		}

		// receive SecretRefName for K8s Secret.
		usePodIdentity := payload.Parameters.UsePodIdentity
		var secretRefName string
		if usePodIdentity == "false" {
			secretRefName = getNodePublishSecretRefFromPod(context.TODO(), pod)
			if secretRefName == "" {
				log.Errorf("Could not find nodePublishSecretRef from pod: %s. Err: %v", pod.GetName(), err)
				return
			}
		}

		// add secrets to payload
		err = buildK8sSecretsPayload(ctx, secretRefName, namespace, &payload)
		if err != nil {
			log.Errorf("Failed to Add Secrets to Payload for spc: %s, err: %v", spcName, err)
		}
		spew.Dump(payload)
		err = fetchProviderBinary(payload)
		if err != nil {
			log.Errorf("Failed to Call Provider Binary. Err: %v", err)
			return
		}
		log.Infof("Finished Calling Binary.")

		log.Infof("Beginning Update of Object Versions.")
		currentObjects, err := readMetadataFiles(payload.TargetPath)
		if err != nil {
			log.Errorf("Failed to Read Metadata files to receive new Object Versions, Err: %v", err)
			continue
		}
		if len(currentObjects) < 1 {
			log.Infof("no object versions to update.")
			continue
		}
		if err := unstructured.SetNestedSlice(spc.Object, currentObjects, "status", "objects"); err != nil {
			log.Errorf("Failed to append object versions to SPC: %s, Err: %v", spc.GetName(), err)
			continue
		}

		client, err := secretsstore.GetClient()
		if err != nil {
			log.Errorf("Failed to get Client")
			continue
		}

		log.Infof("Updating Object Versions in Status for SPC: %s", spc.GetName())
		client.Update(context.TODO(), spc)
		log.Infof("Finished Updating Object Versions for SPC: %s", spc.GetName())
	}

	log.Infof("Finishing Reconciler.")
}

func setProviderTypeInPayload(obj map[string]interface{}, payload *Payload) error {
	provider, err := secretsstore.GetStringFromObjectSpec(obj, "provider")
	if err != nil {
		return err
	}
	payload.Provider = provider
	return nil
}
func buildRunningObjectsInPayload(spcPodStatus *unstructured.Unstructured, payload *Payload) error {
	runningObjects, exists, err := unstructured.NestedSlice(spcPodStatus.Object, "status", "objects")
	if err != nil {
		return fmt.Errorf("Failed to get slice of runningObjects in SPC: %s, Err: %v", spcPodStatus.GetName(), err)
	}
	if !exists {
		return fmt.Errorf("No running Objects in Cluster for spcPodStatus: %s", spcPodStatus.GetName())
	}
	objs := make([]map[string]interface{}, 0)
	for _, obj := range runningObjects {
		o, _ := obj.(map[string]interface{})
		objs = append(objs, o)
	}
	ojsJSON, _ := json.Marshal(objs)
	err = json.Unmarshal(ojsJSON, &payload.CurrentObjects)

	if err != nil {
		return fmt.Errorf("Failed to marshal RunningObjects, Err: %v", err)
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
	return secretsName
}
