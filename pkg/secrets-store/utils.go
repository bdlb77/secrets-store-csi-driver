/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package secretsstore

import (
	corev1 "k8s.io/api/core/v1"

	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"runtime"
	"strings"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v2"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"sigs.k8s.io/secrets-store-csi-driver/apis/v1alpha1"
)

// StringArray ...
type StringArray struct {
	Array []string `json:"array" yaml:"array"`
}

var (
	secretProviderClassGvk = schema.GroupVersionKind{
		Group:   "secrets-store.csi.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "SecretProviderClassList",
	}
)

// getProviderPath returns the absolute path to the provider binary
func (ns *nodeServer) getProviderPath(goos string, providerName string) string {
	if goos == "windows" {
		return normalizeWindowsPath(fmt.Sprintf(`%s\%s\provider-%s.exe`, ns.providerVolumePath, providerName, providerName))
	}
	return fmt.Sprintf("%s/%s/provider-%s", ns.providerVolumePath, providerName, providerName)
}

func normalizeWindowsPath(path string) string {
	normalizedPath := strings.Replace(path, "/", "\\", -1)
	if strings.HasPrefix(normalizedPath, "\\") {
		normalizedPath = "c:" + normalizedPath
	}
	return normalizedPath
}

// getMountedFiles returns all the mounted files names
func getMountedFiles(targetPath string) ([]string, error) {
	var paths []string
	// loop thru all the mounted files
	files, err := ioutil.ReadDir(targetPath)
	if err != nil {
		log.Errorf("failed to list all files in target path %s, err: %v", targetPath, err)
		return nil,
			status.Error(codes.Internal, err.Error())
	}
	sep := "/"
	if strings.HasPrefix(targetPath, "c:\\") {
		sep = "\\"
	} else if strings.HasPrefix(targetPath, `c:\`) {
		sep = `\`
	}
	for _, file := range files {
		paths = append(paths, targetPath+sep+file.Name())
	}
	return paths, nil
}

// getPodUIDFromTargetPath returns podUID from targetPath
func getPodUIDFromTargetPath(targetPath string) string {
	re := regexp.MustCompile(`[\\|\/]+pods[\\|\/]+(.+?)[\\|\/]+volumes`)
	match := re.FindStringSubmatch(targetPath)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// ensureMountPoint ensures mount point is valid
func (ns *nodeServer) ensureMountPoint(target string) (bool, error) {
	notMnt, err := ns.mounter.IsLikelyNotMountPoint(target)
	if err != nil && !os.IsNotExist(err) {
		return !notMnt, err
	}

	if !notMnt {
		// testing original mount point, make sure the mount link is valid
		_, err := ioutil.ReadDir(target)
		if err == nil {
			log.Infof("already mounted to target %s", target)
			// already mounted
			return !notMnt, nil
		}
		if err := ns.mounter.Unmount(target); err != nil {
			log.Errorf("Unmount directory %s failed with %v", target, err)
			return !notMnt, err
		}
		notMnt = true
		// remount it in node publish
		return !notMnt, err
	}

	if runtime.GOOS == "windows" {
		// IsLikelyNotMountPoint always returns notMnt=true for windows as the
		// target path is not a soft link to the global mount
		// instead check if the dir exists for windows and if it's not empty
		// If there are contents in the dir, then objects are already mounted
		f, err := ioutil.ReadDir(target)
		if err != nil {
			return !notMnt, err
		}
		if len(f) > 0 {
			notMnt = false
			return !notMnt, err
		}
	}

	return false, nil
}

// GetClient returns client.Client
func GetClient() (client.Client, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: nil, Mapper: nil})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetSecretProviderItem returns the secretproviderclass object by name and namespace
func GetSecretProviderItem(ctx context.Context, name, namespace string) (*unstructured.Unstructured, error) {
	instanceList := &unstructured.UnstructuredList{}
	instanceList.SetGroupVersionKind(secretProviderClassGvk)
	// recreating client here to prevent reading from cache
	c, err := GetClient()
	if err != nil {
		return nil, err
	}
	err = c.List(ctx, instanceList, &client.ListOptions{Namespace: namespace})
	if err != nil {
		return nil, err
	}

	for _, item := range instanceList.Items {
		if item.GetName() == name {
			return &item, nil
		}
	}
	return nil, fmt.Errorf("could not find secretproviderclass %s", name)
}

func GetStringFromObjectSpec(object map[string]interface{}, key string) (string, error) {
	value, exists, err := unstructured.NestedString(object, "spec", key)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("could not get field %s from spec", key)
	}
	if len(value) == 0 {
		return "", fmt.Errorf("field %s is not set", key)
	}
	return value, nil
}

// GetStringFromObjectStatus used in Reconciler
func GetStringFromObjectStatus(object map[string]interface{}, key string) (string, error) {
	value, exists, err := unstructured.NestedString(object, "status", key)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("could not get field %s from status", key)
	}
	if len(value) == 0 {
		return "", fmt.Errorf("field %s is not set", key)
	}
	return value, nil
}
func GetMapFromObjectSpec(object map[string]interface{}, key string) (map[string]string, error) {
	value, exists, err := unstructured.NestedStringMap(object, "spec", key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("could not get field %s from spec", key)
	}
	if len(value) == 0 {
		return nil, fmt.Errorf("field %s is not set", key)
	}
	return value, nil
}

func createSecretProviderClassPodStatus(ctx context.Context, podname, namespace, podUID, spcName, targetPath, nodeID string, mounted bool) error {
	obj := &unstructured.Unstructured{}
	obj.SetName(podname + "-" + namespace + "-" + spcName)
	obj.SetNamespace(namespace)
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   v1alpha1.GroupVersion.Group,
		Version: v1alpha1.GroupVersion.Version,
		Kind:    "SecretProviderClassPodStatus",
	})
	// Set owner reference to the pod as the mapping between secret provider class pod status and
	// pod is 1 to 1. When pod is deleted, the spc pod status will automatically garbage collected
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       podname,
			UID:        types.UID(podUID),
		},
	})

	status := map[string]interface{}{
		"podName":                 podname,
		"targetPath":              targetPath,
		"mounted":                 mounted,
		"secretProviderClassName": spcName,
	}
	if err := unstructured.SetNestedField(
		obj.Object, status, "status"); err != nil {
		return err
	}

	obj.SetLabels(map[string]string{
		"internal.secrets-store.csi.k8s.io/node-name": nodeID,
	})
	// recreating client here to prevent reading from cache
	c, err := GetClient()
	if err != nil {
		return err
	}
	// create the secret provider class pod status
	err = c.Create(ctx, obj, &client.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// GetK8sSecret used in Reconciler Package
func GetK8sSecret(ctx context.Context, name string, namespace string) (*corev1.Secret, error) {
	client, err := GetClient()

	if err != nil {
		return nil, err
	}
	secretKey := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
	}

	err = client.Get(ctx, secretKey, secret)

	return secret, nil
}

func initializeObjectsInStatus(ctx context.Context, item *unstructured.Unstructured, parameters map[string]string, runningObjects []interface{}) error {
	var objectStr string
	for k, v := range parameters {
		if k == "objects" {
			objectStr = v
			break
		}
	}
	var objectsA StringArray
	err := yaml.Unmarshal([]byte(objectStr), &objectsA)
	if err != nil {
		log.Infof("unmarshal failed for objects")
		return err
	}
	log.Debugf("objects array: %v", objectsA.Array)

	for i, o := range objectsA.Array {
		var runningObject map[string]interface{}
		err = yaml.Unmarshal([]byte(o), &runningObject)
		if err != nil {
			log.Infof("unmarshal failed for Running Objects at index %d", i)
			return err
		}
		delete(runningObject, "objectType")
		delete(runningObject, "objectAlias")
		runningObjects = append(runningObjects, runningObject)
	}
	log.Infof("unmarshaled Running Objects: %v", runningObjects)
	if err := unstructured.SetNestedSlice(item.Object, runningObjects, "status", "objects"); err != nil {
		log.Infof("Set Nested field failed for Status and Running Objects.")
		return err
	}

	client, err := GetClient()
	client.Update(ctx, item)
	return nil
}
