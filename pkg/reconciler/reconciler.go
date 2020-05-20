// create/update objectVersions with mounted file content
// add versions info to secretProviderClass obj's versions status field

package reconciler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	corev1 "k8s.io/api/core/v1"

	"github.com/davecgh/go-spew/spew"
	"github.com/prometheus/common/log"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"
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
	Provider     string `json:"provider" yaml:"provider"`
	TargetPath   string
	Parameters   Parameters `json:"parameters" yaml:"parameters"`
	Secrets      Secrets
	ProviderName string `json:"name" yaml:"name"`
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
	log.Infof("Retrieving list of Secret Provider Classes in the cluster.")
	secretProviderClassObjects, err := secretsstore.ListSecretProviderClasses(ctx)
	if len(secretProviderClassObjects.Items) < 1 {
		log.Infof("Did not find any Secret Provider Classes in the cluster.")
		log.Infof("Reconciler Exiting.")
		return
	}
	if err != nil {
		log.Errorf("Issues fetching list of Secret Provider Classes: %v", err)
	}
	for _, item := range secretProviderClassObjects.Items {
		payload := &Payload{
			Parameters: Parameters{},
			Secrets:    Secrets{},
		}
		err := buildPayloadFromSecretProviderClass(ctx, item, payload)
		if err != nil {
			log.Errorf("Could not build provider payload for SPC: %v, Error: %v", item.GetName(), err)
			return
		}
		log.Infof("Invoking Provider Binary")
		err = fetchProviderBinary(payload)
		if err != nil {
			log.Errorf("Error when Calling Provider. Error: %v", err)
			return
		}
		log.Infof("Finished Provider Binary")
	}
}
func buildPayloadFromSecretProviderClass(ctx context.Context, item unstructured.Unstructured, payload *Payload) error {

	spcName := item.GetName()
	payload.ProviderName = spcName
	parameters, err := secretsstore.GetMapFromObjectSpec(item.Object, "parameters")
	if err != nil {
		return fmt.Errorf("Issue receiving params from instance of SPC: %v, Error: %v ", spcName, err)
	}

	paramsJSON, err := json.Marshal(parameters)
	err = json.Unmarshal(paramsJSON, &payload.Parameters)
	if err != nil {
		return fmt.Errorf("Issue Unmarshaling params from instance of SPC: %v, Error: %v ", spcName, err)
	}

	if err != nil {
		return fmt.Errorf("Could not convert parameters of SPC: %v to JSON, Error: %v", spcName, err)
	}

	if err != nil {
		return fmt.Errorf("Issue slicing params from SPC: %v, Error: %v", spcName, err)
	}

	providerType, _, err := unstructured.NestedString(item.Object, "spec", "provider")
	payload.Provider = providerType
	if err != nil {
		return fmt.Errorf("Issue slicing provider from SPC: %v, Error: %v", spcName, err)
	}

	usePodIdentity := parameters["usePodIdentity"]
	err = podInfoToPayload(ctx, payload, item.Object, spcName, usePodIdentity)
	if err != nil {
		return fmt.Errorf("podInfoToPayload(): %v", err)
	}

	return nil
}
func fetchProviderBinary(payload *Payload) error {
	providerBinary := fmt.Sprintf("%s/%s/provider-%s", providerVolumePath, payload.Provider, payload.Provider)
	spew.Dump(payload)
	parametersStr, err := json.Marshal(&payload.Parameters)
	if err != nil {
		log.Errorf("failed to marshal parameters")
		return err
	}
	secretStr, err := json.Marshal(&payload.Secrets)
	if err != nil {
		log.Errorf("failed to marshal secrets")
		return err
	}

	args := []string{
		"--attributes", string(parametersStr),
		"--secrets", string(secretStr),
		"--targetPath", string(payload.TargetPath),
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

func addK8sSecretsToPayload(ctx context.Context, secretsName, namespace string, payload *Payload) error {
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

func podInfoToPayload(ctx context.Context, payload *Payload, obj map[string]interface{}, spcName string, usePodIdentity string) error {
	log.Infof("Fetching Pods From Status Field in SPC: %v", spcName)
	statusPods, exists, err := unstructured.NestedSlice(obj, "status", "byPod")
	if err != nil {
		return fmt.Errorf("Issue parsing byPod information in the Status Field for Secret Provider Class: %v, Error: %v", spcName, err)
	}
	if !exists {
		return fmt.Errorf("No pods tracked in Status Field for Secret Provider Class: %v, Error: %v", spcName, err)
	}

	if err != nil {
		return fmt.Errorf("Issue slicing params from SPC: %v, Error: %v", spcName, err)
	}
	for _, p := range statusPods {
		podMap, _ := p.(map[string]interface{})
		name, namespace := podMap["name"].(string), podMap["namespace"].(string)

		podKey := types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}
		pod := &corev1.Pod{}
		client, err := secretsstore.GetClient()
		if err != nil {
			return fmt.Errorf("issue with creating client, Error: %v", err)
		}
		err = client.Get(ctx, podKey, pod)
		if err != nil {
			return fmt.Errorf("Issue Fetching Pod from API, Error: %v", err)
		}
		podUID := pod.UID
		var secretsName, mountName string
		for _, vol := range pod.Spec.Volumes {
			if vol.CSI == nil {
				continue
			}
			// TODO: check type of driver
			if usePodIdentity == "false" {
				secretsName = vol.CSI.NodePublishSecretRef.Name
			}
			mountName = vol.Name
		}
		payload.TargetPath = fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes.io~csi/%s/mount", podUID, mountName)

		log.Infof("Adding K8s Secrets to Payload for SPC: %v", spcName)
		if usePodIdentity == "false" {
			err = addK8sSecretsToPayload(ctx, secretsName, namespace, payload)
			if err != nil {
				return fmt.Errorf("Issue forming K8s secrets inside payload Struct. error: %v", err)
			}
		}
	}
	return nil
}
