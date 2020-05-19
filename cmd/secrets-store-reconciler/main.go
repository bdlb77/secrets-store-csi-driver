package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"
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
	Provider     string     `json:"provider" yaml:"provider"`
	MountPath    string     `json:"mountPath" yaml:"mountPath"`
	Parameters   Parameters `json:"parameters" yaml:"parameters"`
	Permissions  int        `json:"permissions" yaml:"permissions"`
	Secrets      Secrets
	ProviderName string `json:"name" yaml:"name"`
}
type Secrets struct {
	ClientID     string `json:"clientid" yaml:"clientid"`
	ClientSecret string `json:"clientsecret" yaml:"clientsecret"`
}

const (
	parametersField                = "paramaters"
	permission         os.FileMode = 0644
	providerName                   = "azure"
	providerVolumePath             = "/etc/kubernetes/secrets-store-csi-providers"
)

func main() {
	var ctx context.Context

	// TODO: Refactor to use Client
	log.Infof("Retrieving list of Secret Provider Classes in the cluster.")
	instanceList, err := secretsstore.ListSecretProviderClasses(ctx)
	if err != nil {
		log.Errorf("Issues fetching list of Secret Provider Classes: %v", err)
	}
	for _, item := range instanceList.Items {
		payload := &Payload{
			Parameters: Parameters{},
			Secrets:    Secrets{},
		}
		err := buildPayloadFromSPC(ctx, item, payload)
		if err != nil {
			log.Errorf("Could not build provider payload for SPC: %v, Error: %v", item.GetName(), err)
		}

		err = fetchProviderBinary(payload)
		if err != nil {
			log.Errorf("Error when Calling Provider. Error: %v", err)
		}

	}
}
func buildPayloadFromSPC(ctx context.Context, item unstructured.Unstructured, payload *Payload) error {

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
	err = podInfoToPayload(ctx, payload, item.Object, spcName, parameters["usePodIdentity"])
	if err != nil {
		return fmt.Errorf("podInfoToPayload(): %v", err)
	}
	return nil
}
func fetchProviderBinary(payload *Payload) error {
	providerVolumePath := "/etc/kubernetes/secrets-store-csi-providers"
	// /etc/kubernetes/secrets-store-csi-providers/azure/provider-azure
	providerBinary := fmt.Sprintf("%s/%s/provider-%s", providerVolumePath, payload.Provider, payload.Provider)

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
	permissionStr, err := json.Marshal(&payload.Permissions)
	if err != nil {
		log.Errorf("failed to marshal file permission")
		return err
	}

	args := []string{
		"--attributes", string(parametersStr),
		"--secrets", string(secretStr),
		"--targetPath", string(payload.MountPath),
		"--permission", string(permissionStr),
	}
	log.Infof("provider command invoked: %s %s %v", providerBinary,
		"--attributes [REDACTED] --secrets [REDACTED]", args[4:])
	cmd := exec.Command(
		providerBinary,
		args...,
	)
	spew.Dump(args)
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

		var secretsName, mountName string
		for _, vol := range pod.Spec.Volumes {
			if vol.CSI == nil {
				continue
			}
			secretsName = vol.CSI.NodePublishSecretRef.Name
			mountName = vol.Name
		}

		// TODO: Think to refactor
		for _, c := range pod.Spec.Containers {
			for _, volM := range c.VolumeMounts {
				if mountName == volM.Name {
					payload.MountPath = volM.MountPath
					if volM.ReadOnly {
						payload.Permissions = 0644
					}
				}
			}
		}
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
