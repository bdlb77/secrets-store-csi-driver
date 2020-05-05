// create/update objectVersions with mounted file content
// add versions info to secretProviderClass obj's versions status field

package reconciler

import (
	"fmt"
	"net/http"

	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Provider Params to be used in Provider to call Vault.
type ProviderParams struct {
	KeyVaultName string
	Objects      []KeyVaultObject
	TenantID     string
	// TODO: can retrieve from K8s secrets?
	AADClientSecret string
	AADClientID     string
}

// Object representing each secret inside a Vault.
type KeyVaultObject struct {
	ObjectName    string `json: "objectName" yaml: "objectName"`
	ObjectAlias   string `json: "objectAlias" yaml: "objectAlias"`
	ObjectType    string `json: "objectType" yaml: "objectType"`
	ObjectVersion string `json: "objectVersion" yaml: "objectVersion"`
}

const (
	parametersField = "paramaters"
)

var (
	secretProviderClassGvk = schema.GroupVersionKind{
		Group:   "secrets-store.csi.x-k8s.io",
		Version: "v1alpha1",
		Kind:    "SecretProviderClassList",
	}
)

func getObjectVersions(ctx context.Context, obj *unstructured.Unstructured) ([]ProviderParams, error) {
	// query client and get list of SPC

	c, err := getClient()
	if err != nil {
		return nil, err
	}

	instanceList := &unstructured.UnstructuredList{}
	instanceList.SetGroupVersionKind(secretProviderClassGvk)
	err = c.List(ctx, instanceList)
	if err != nil {
		return nil, err
	}

	objects, _, err := unstructured.NestedSlice(obj.Object, "spec", parametersField, "objects")

	if err != nil {
		return nil, err
	}

	for i, o := range objects {
		fmt.Println("Object %d: %v", i, o)
	}
	// item, err := getSecretProviderItemByName(ctx, secretProviderClass)

	return nil, nil
}

func getClient() (client.Client, error) {
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

// call manually
func manualCall() {
	var token = "eyJhbGciOiJSUzI1NiIsImtpZCI6IjhmTGZDa0U3cXJsVjFmajd5M24yY0x6aE9RNlMxUW1NSk5la2kxbWFqUFEifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InNlY3JldHMtc3RvcmUtY3NpLWRyaXZlci10b2tlbi1kcm5rNiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiIyZmY5ODQ0NS02ZTU3LTRiMDUtOWRkMy0yODhiMjg1MjM4MGMiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIifQ.o1aoQD2axVXMY9f7-5PNWF5TG9xsQBHbSVycLaT2gZ09UUkvyN-RV83I4JXHSuBzWHzakjMs6EU4Hbmk0ok6DpNzPowcelWIATvJlrEreyg36xKqTvLX8BZiFAUyP_fz_vx5v7tERfdYNcTQbBxk09x_ZvN0iREe0potJA6c3RNWDh6k1hhwPPt769zvNFJqaQmNfOSy0vacVSYFVCjKqmwXMJ7N1sHCyNL0Z5gAUH2UBfgNPvTr8K2et6kVhwaQXtPlhzosodjtNzPyHzl9Os83DcViYqh2V-wFlYlCUa60V36K2YDWCMbkCXtjJeibBXT3wR6g73RyGf1ezgIeoCGDOxhGkeTK87cHP2P1A965AEUZATfc8ihcO_4VcMDmOZWCMMXTPkXnOlWE3jgFIUUkOSbpI6ML9551IPOkoXlp_-0A7wMf3HATRxhAd4S_Nzj14Wfe4YN3WbSeeFt9_uScz6DZeL9UiH76iHUnLgHARxzt-onJjAW-uC950Ngi7Re2u3O-yTgRxlYDvjLtt_DUDe1oYI2TLH2Ou4r63QSzMyE5oh6Kqg-Dv4Eth2LVoHpLF7zCET8d7pFunn_lHe06NBWdunPAaAe9OLAJbqXJlZHPSJyDe9LFUIoD_OZY8dRHg6vXIWLSxf-9WJtiuIJF5iQXOro0AR4AukcR2CA"
	req, err := http.NewRequest("GET", "https://kubernetes.default.svc/apis/secrets-store.csi.x-k8s.io/v1alpha1/secretproviderclasses", nil)
	if err != nil {
		fmt.Printf("Error: %s", err)
	}
	req.Header.Set("AUthorization", "Bearer "+token)

	response, err := http.Get("https://kubernetes.default.svc/apis/secrets-store.csi.x-k8s.io/v1alpha1/secretproviderclasses")
	//  curl -v --insecure  -H "Authorization: Bearer $(cat  /var/run/secrets/kubernetes.io/serviceaccount/token)" https://kubernetes.default.svc/apis/secrets-store.csi.x-k8s.io/v1alpha1/secretproviderclasses
}
