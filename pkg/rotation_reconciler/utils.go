package rotation_reconciler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1alpha1 "sigs.k8s.io/secrets-store-csi-driver/apis/v1alpha1"
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

func getSPC(ctx context.Context, namespace, name string, obj *v1alpha1.SecretProviderClass) error {

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
func getSecretProviderClassPodStatuses(ctx context.Context) (*unstructured.UnstructuredList, error) {
	bindingList := &unstructured.UnstructuredList{}
	bindingList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   v1alpha1.GroupVersion.Group,
		Version: v1alpha1.GroupVersion.Version,
		Kind:    "SecretProviderClassPodStatusList",
	})

	// recreating client here to prevent reading from cache
	c, err := secretsstore.GetClient()
	if err != nil {
		return nil, err
	}
	err = c.List(ctx, bindingList)
	if err != nil {
		return nil, err
	}

	return bindingList, nil
}
