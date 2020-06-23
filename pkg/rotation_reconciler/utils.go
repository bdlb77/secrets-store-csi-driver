package rotationreconciler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os/exec"

	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"
)

func getPod(ctx context.Context, namespace, name string, obj *corev1.Pod) error {
	client, err := secretsstore.GetClient()
	if err != nil {
		return err
	}
	objectKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err = client.Get(ctx, objectKey, obj)
	if err != nil {
		return err
	}

	return nil
}

func getSPC(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	spc := &unstructured.Unstructured{}
	SecretProviderClassGvk := schema.GroupVersionKind{
		Group:   "secrets-store.csi.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "SecretProviderClass",
	}
	spc.SetGroupVersionKind(SecretProviderClassGvk)

	client, err := secretsstore.GetClient()
	if err != nil {
		return nil, err
	}
	objectKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err = client.Get(ctx, objectKey, spc)
	if err != nil {
		return nil, err
	}

	return spc, nil
}
func getSecretProviderClassPodStatuses(ctx context.Context) (*unstructured.UnstructuredList, error) {
	// recreating client here to prevent reading from cache
	c, err := secretsstore.GetClient()
	if err != nil {
		return nil, err
	}

	bindingList := &unstructured.UnstructuredList{}
	SecretProviderClassGvk := schema.GroupVersionKind{
		Group:   "secrets-store.csi.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "SecretProviderClassPodStatusList",
	}
	bindingList.SetGroupVersionKind(SecretProviderClassGvk)

	err = c.List(ctx, bindingList)
	if err != nil {
		return nil, err
	}

	return bindingList, nil
}
func fetchProviderBinary(payload Payload) error {
	providerBinary := fmt.Sprintf("%s/%s/provider-%s", providerVolumePath, payload.Provider, payload.Provider)
	parametersStr, err := json.Marshal(payload.Parameters)
	if err != nil {
		return fmt.Errorf("failed to marshal parameters, Err: %v", err)
	}
	objectVersionStr, err := json.Marshal(payload.CurrentObjects)
	if err != nil {
		return fmt.Errorf("Failed to marshal list of current ObjectVersions present in cluster, Err: %v", err)
	}
	secretStr, err := json.Marshal(payload.Secrets)
	if err != nil {
		return fmt.Errorf("failed to marshal secrets, Err: %v", err)
	}

	args := []string{
		"--attributes", string(parametersStr),
		"--secrets", string(secretStr),
		"--targetPath", string(payload.TargetPath),
		"--objectVersions", string(objectVersionStr),
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

func readMetadataFiles(targetPath string) ([]interface{}, error) {
	path := targetPath + "/.metadata"
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to list all files in target path %s, err: %v", path, err)
	}
	if len(files) < 1 {
		return nil, fmt.Errorf("No Objects Written by The Provider at path: %s", path)
	}
	newObjects := make([]interface{}, 0)
	for _, file := range files {
		content, err := ioutil.ReadFile(path + "/" + file.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s, err: %v", file.Name(), err)
		}
		// format string to take in correct value from file
		str := string(content[:len(content)-1])
		obj := map[string]interface{}{
			"objectName":    file.Name(),
			"objectVersion": str,
		}
		newObjects = append(newObjects, obj)
	}
	return newObjects, nil
}
