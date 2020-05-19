package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/apimachinery/pkg/types"
	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"
)

// KeyVaultObject representing each secret inside a Vault.
type KeyVaultObject struct {
	ObjectName    string `json:"objectName" yaml:"objectName"`
	ObjectAlias   string `json:"objectAlias" yaml:"objectAlias"`
	ObjectType    string `json:"objectType" yaml:"objectType"`
	ObjectVersion string `json:"objectVersion" yaml:"objectVersion"`
}

const (
	parametersField                = "paramaters"
	permission         os.FileMode = 0644
	providerName                   = "azure"
	providerVolumePath             = "/etc/kubernetes/secrets-store-csi-providers"
)

func main() {
	// handle()
	var ctx context.Context
	// TODO: Refactor this out with Client Library.
	providerNames, err := fetchSecretProviderClasses(ctx)
	if err != nil {
		log.Errorf("issue with getting provider classes, %v", err)
	}

	for _, name := range providerNames {
		item, _ := secretsstore.GetSecretProviderItemByName(ctx, name)
		// paramStr, err := secretsstore.GetMapFromObjectSpec(item.Object, "parameters")
		if err != nil {
			log.Errorf("Issue Marshaling Provider from Secret Provider Class: %v", name)
		}

		podInfo, exists, err := unstructured.NestedSlice(item.Object, "status", "byPod")
		if err != nil {
			log.Errorf("Issue parsing byPod information in the Status Field for Secret Provider Class: %v", name)
		}
		if !exists {
			log.Errorf("No pods tracked in Status Field for Secret Provider Class: %v", name)
		}

		for _, p := range podInfo {
			podMap, _ := p.(map[string]interface{})

			// name, namespace := podMap["name"], podMap["namespace"]
			name, namespace := podMap["name"].(string), podMap["namespace"].(string)
			spew.Dump(name)
			podKey := types.NamespacedName{
				Namespace: namespace,
				Name:      name,
			}
			pod := &corev1.Pod{}

			client, err := secretsstore.GetClient()
			err = client.Get(ctx, podKey, pod)
			if err != nil {
				log.Errorf("Issue with creating client: %v", err)
			}
			var secretsName, mountName, mountPath string
			var permissions int
			for _, vol := range pod.Spec.Volumes {
				if vol.CSI == nil {
					continue
				}
				secretsName = vol.CSI.NodePublishSecretRef.Name
				mountName = vol.Name
			}
			for _, c := range pod.Spec.Containers {
				for _, volM := range c.VolumeMounts {
					if mountName == volM.Name {
						mountPath = volM.MountPath
						if volM.ReadOnly {
							permissions = 0644
						}
					}
				}
			}

			secret, err := secretsstore.GetK8sSecret(ctx, secretsName, namespace)
			if err != nil {
				log.Errorf("ISsue with fetching K8s Secret: %v", err)
			}
			// secretsName
			// volumes, exists, err := &corev1.Po(pod, "volumes", "csi")
			k8sSecrets := make(map[string]string)
			for k, v := range secret.Data {
				k8sSecrets[k] = string(v)
			}
			// secret.Unmarshal(&k8sSecrets)
			spew.Dump(k8sSecrets, permissions, mountPath)
		}
		// Find podNames
		//  For each name (--> run concurrently)
		// fetch Pod w/ pod name
		// if usePodIdentity false
		// take secretRef create secrets..
		// getK8s secret
		// find mounth path

		// params = append(params, paramStr)
		// items = append(items, item)
		// statuses = append(statuses, podInfo)

	}
}

func handle() {
	log.Infof("Hello from Reconciler")
}

func fetchSecretProviderClasses(ctx context.Context) ([]string, error) {
	// serviceHost := os.GetEnv("KUBERNETES_SERVICE_HOST")
	// servicePort := os.GetEnv("KUBERNETES_SERVICE_PORT")
	apiVersion := "v1alpha1"            // For example
	namespace := "default"              // For example
	resource := "secretproviderclasses" // For example
	httpMethod := http.MethodGet        // For Example

	urlString := fmt.Sprintf("http://localhost:8001/apis/secrets-store.csi.x-k8s.io/%s/namespaces/%s/%s", apiVersion, namespace, resource)

	req, err := fetchK8sAPI(urlString, httpMethod)

	// caToken, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	caToken := "eyJhbGciOiJSUzI1NiIsImtpZCI6IjhmTGZDa0U3cXJsVjFmajd5M24yY0x6aE9RNlMxUW1NSk5la2kxbWFqUFEifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InNlY3JldHMtc3RvcmUtY3NpLWRyaXZlci10b2tlbi1rZDJrZyIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiI0MDlmNDhkNy1iODZiLTQxNDktOWU0ZC1hNzg4ODY3YjBlZWQiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIifQ.zRTqllCFjDWHvzAIjJbcLhmiYFwPlWTwV2Hd28YvN04tnY6Hb6YlKduC3urT-oGTPsZDRoiU_nZhhyKHbSXbOQarPC1UJTo98QimvssP4HAGj5BykCgd_Ht8Uti4bdWXlE1zQIi3TAgAL2iIFNxOxkal6TxMPo4oQttHEyo4Sdi1BtcAGG_0nGTh05ImihL0Ljwz5v3d3BniOhiqPrhiEjDBJvBCcqZMifTe_HPXLK_TJU1Io2n_IU6V2AyRqGq3BhGfDsSTTfyR1yOCgwZnAxlwy9vzp9vnQwDFuEElbAUfgqoPvinnY-mvEimofnEhVSA23GEiG3XHvio4biomonzbDum-1NbS0cqJvM87XQEdFmww8jqmFxJV1ILi5i15GITQpwOAIqMYhauKDPQXM0eH_WaZSg5XVu9HgpuVK9RFL0HdrfZVMYsV1q1IiUZJzh_tps0Jl6wn1cQuK0KbUZSVFuQDpuGJGG5rWl7MubfFa0SdLCBBW3HIhclnGqr4ENjKtqHsXVzxbJFojC8K1o8wtpIENMUxvGlcrmnMF3iINZQsRsCaK"
	if err != nil {
		panic(err) // cannot find token file
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", string(caToken)))

	caCertPool, err := getCert()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Fetching Secret Provider Classes Failed.: %s", err.Error())
		return []string{}, err
	}

	if resp.StatusCode == 403 {
		log.Errorf("Fetching %s is Forbidden. Error: %s", resource, err.Error())
	}
	defer resp.Body.Close()
	resBodyBytes, _ := ioutil.ReadAll(resp.Body)

	results := make(map[string]interface{})
	_ = json.Unmarshal(resBodyBytes, &results)
	// spew.Dump(results)

	var data Data
	err = json.Unmarshal(resBodyBytes, &data)

	if err != nil {
		return nil, err
	}

	var providerNames []string
	for _, provider := range data.Items {
		providerNames = append(providerNames, provider.MetaData.Name)
	}

	// *  Don't Parse Objects b/c will need to pass serialized to provider.
	//// keyVaultObjects, err := parseObjectsFromAPI(providerAttributes)
	// _, err = fetchSecrets()

	// err = fetchProviderBinary(providerAttributes, secrets)

	return providerNames, nil
}

func fetchK8sAPI(urlStr string, httpMethod string) (*http.Request, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(httpMethod, u.String(), nil)
	if err != nil {
		return nil, err
	}

	// caToken, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	caToken := "eyJhbGciOiJSUzI1NiIsImtpZCI6IjhmTGZDa0U3cXJsVjFmajd5M24yY0x6aE9RNlMxUW1NSk5la2kxbWFqUFEifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6InNlY3JldHMtc3RvcmUtY3NpLWRyaXZlci10b2tlbi1rZDJrZyIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC51aWQiOiI0MDlmNDhkNy1iODZiLTQxNDktOWU0ZC1hNzg4ODY3YjBlZWQiLCJzdWIiOiJzeXN0ZW06c2VydmljZWFjY291bnQ6ZGVmYXVsdDpzZWNyZXRzLXN0b3JlLWNzaS1kcml2ZXIifQ.zRTqllCFjDWHvzAIjJbcLhmiYFwPlWTwV2Hd28YvN04tnY6Hb6YlKduC3urT-oGTPsZDRoiU_nZhhyKHbSXbOQarPC1UJTo98QimvssP4HAGj5BykCgd_Ht8Uti4bdWXlE1zQIi3TAgAL2iIFNxOxkal6TxMPo4oQttHEyo4Sdi1BtcAGG_0nGTh05ImihL0Ljwz5v3d3BniOhiqPrhiEjDBJvBCcqZMifTe_HPXLK_TJU1Io2n_IU6V2AyRqGq3BhGfDsSTTfyR1yOCgwZnAxlwy9vzp9vnQwDFuEElbAUfgqoPvinnY-mvEimofnEhVSA23GEiG3XHvio4biomonzbDum-1NbS0cqJvM87XQEdFmww8jqmFxJV1ILi5i15GITQpwOAIqMYhauKDPQXM0eH_WaZSg5XVu9HgpuVK9RFL0HdrfZVMYsV1q1IiUZJzh_tps0Jl6wn1cQuK0KbUZSVFuQDpuGJGG5rWl7MubfFa0SdLCBBW3HIhclnGqr4ENjKtqHsXVzxbJFojC8K1o8wtpIENMUxvGlcrmnMF3iINZQsRsCaK"
	if err != nil {
		return nil, err // cannot find token file
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", string(caToken)))

	return req, nil
}

// func parseObjectsFromAPI(apiData Parameter) ([]KeyVaultObject, error) {
//  	stringifiedObjs := apiData.Objects

//  	var objString StringArray
//  	if err := yaml.Unmarshal([]byte(stringifiedObjs), &objString); err != nil {
//  		log.Errorf("Failed to Unmarshal YAML for Stringified Object of %v", stringifiedObjs)
//  		return nil, err
//  	}

//  	keyVaultObjects := []KeyVaultObject{}
//  	for i, o := range objString.Array {
//  		var keyVaultObject KeyVaultObject
//  		err := yaml.Unmarshal([]byte(o), &keyVaultObject)
//  		if err != nil {
//  			log.Errorf("Failed to Unmarshal YAML for %v, at index %d", o, i)
//  			return nil, err
//  		}
//  		keyVaultObjects = append(keyVaultObjects, keyVaultObject)
//  	}

//  	return keyVaultObjects, nil
// }

// permission         os.FileMode = 0644
// 	providerName                   = "azure"
// 	providerVolumePath             = "/etc/kubernetes/secrets-store-csi-providers"

// func fetchProviderBinary(parameters Parameter, secrets Secrets) error {

// 	providerBinary := fmt.Sprintf("%s/%s/provider-%s", providerVolumePath, providerName, providerName)

// 	parametersStr, err := json.Marshal(parameters)
// 	if err != nil {
// 		log.Errorf("failed to marshal parameters")
// 		return err
// 	}
// 	secretStr, err := json.Marshal(secrets)
// 	if err != nil {
// 		log.Errorf("failed to marshal secrets")
// 		return err
// 	}
// 	permissionStr, err := json.Marshal(permission)
// 	if err != nil {
// 		log.Errorf("failed to marshal file permission")
// 		return err
// 	}

// 	args := []string{
// 		"--attributes", string(parametersStr),
// 		"--secrets", string(secretStr),
// 		"--targetPath", string("/secrets"),
// 		"--permission", string(permissionStr),
// 	}
// 	spew.Dump(args)
// 	spew.Dump(providerBinary)

// 	// log.Infof("provider command invoked: %s %s %v", providerBinary,
// 	// 	"--attributes [REDACTED] --secrets [REDACTED]", args[4:])
// 	// cmd := exec.Command(
// 	// 	providerBinary,
// 	// 	args...,
// 	// )

// 	// stdout := &bytes.Buffer{}
// 	// stderr := &bytes.Buffer{}
// 	// cmd.Stderr, cmd.Stdout = stderr, stdout

// 	// if err := cmd.Run(); err != nil {
// 	// 	return err
// 	// }
// 	return nil
// }

func getCert() (*x509.CertPool, error) {
	caCertPool := x509.NewCertPool()
	// caCert, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	// if err != nil {
	// 	return nil, err
	// }
	caCert := `-----BEGIN CERTIFICATE-----
MIIEyjCCArKgAwIBAgIRAKE2a7aC9QAUVhj9pMuknHUwDQYJKoZIhvcNAQELBQAw
DTELMAkGA1UEAxMCY2EwIBcNMjAwNTAxMTkwMjQ3WhgPMjA1MDA1MDExOTEyNDda
MA0xCzAJBgNVBAMTAmNhMIICIjANBgkqhkiG9w0BAQEFAAOCAg8AMIICCgKCAgEA
yXag52+ZGol1UaUJu9ts9KIH+A4oxecOnJVFJfjrGaMStjTSon7402pqOEnlWvNK
T6W1z11T+A2atNOd0NHZMTAmOg4TA2LsxhIaSYGmJJdoIIIYS1qJoUxpp3d4CGt8
IvwkusiGk5XEMEFFHyyehoAZQSxbnD7G0EbxuHGh7BVeYPJP3eKNwO4LZMuTvg6Z
Y4K5NedqTv2ZbQEpNsgT6cMYI/iklecGt0pHz7sEJ3ojHmYemUj/UPD2H2l89/Wy
P9fbSI8xDfr8U4EmG1Mkv9X+AwtUEhdWmsC5ePESAfPQELlLFw/phLuV0smpVNQz
bfjYQioOqTAZYYBfvkOki9TzZgukrmhGwbndBjGiynbNh/SfzIl9cBBbAfb6KOES
8YjzNO7/XSRDUprDn6Uc1biLxJ670LEVSWmqpycfUSm6VYOy+c1ntMfEb9yVb1+3
0ruoyRCN8IAlbGd+0gbPGXRa/yriXVLCa0E5M/7vow7ue3+lFwFRANUy8MCJq+Sx
xraWtgHfK7nVvU54z9f+7h+MEJ7hSughKAcu/RwYLJWJ5NWWggt+Dx5KOqNB823t
l44cgoBp9na8fbkOEElmMeh/pt3sGoaMgvsNhRUgHJF/EvV2OGkPwBvTO2OIK81s
V2aSHlQBo/iv6Zvo32IuNHbsOjIsZ8gFr+qsIXQKHH0CAwEAAaMjMCEwDgYDVR0P
AQH/BAQDAgKkMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggIBAHOM
Sr07vumXw3bdq/Nt7xbXHSCAtEXlm0YsaU/9DaruQExIEP89bEEZTnNcq5GU4PgW
mccBcsoJnl6NsvymbhqA18Aha+kwRapFPwJe0kUvvG4QqlTTUN3jlaZ8wHuLnYG4
+so4yvvLtCtkSoZdV67iDTuhMduCMMJ5ALwsmWjHVN3aBKTdHrIGgPmo065HB/2m
FkEGM+Zu/X0LZcYsWDmCLVBzvTuQ0th1KOT0STgO725o2gEKQl6Z9m8CNWKMbuAM
mFvjupcQ/ZCFgek460R58HS+tQFcbAnLgbZKGcnOuSmETo9BmWm67N1yWL8kQHal
3Gl29f8VcrcbPAkl/SCvEakoozBZDcpKW1jWf1Q6bWqm4bZ0RpQqqb6y7E4JXLjs
/vcC6Wc87YuLcZyN+91FPKDrhfkXzxyuiHk12c1bYaZSNuskjuKx4rNylowVwul3
FbmFpUXXhNTET+Xn9DPKBXwGQz13erpka2n6B+hMJGx5wTUKMQfYV3i2bZwkYYcL
IYl/vtqHduETYEFL+B+gJJ2HpV9yiGdLCoq1rnZ6VehnWT6r31hQpDHcZmD7CQla
C8fSPx49nhUTHjK4HS15rjTPEylJqHU7g3tFeFUE1ny41x9kGnscvyaiL9fv+7Le
KmsuAvQWIj/nk40KepWoZh7HiSX2xeFR1VLCvQHb
-----END CERTIFICATE-----`

	caCertPool.AppendCertsFromPEM([]byte(caCert))
	return caCertPool, nil
}
