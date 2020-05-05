package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/davecgh/go-spew/spew"
	log "github.com/sirupsen/logrus"
)

// ProviderParams to be used in Provider to call Vault.
type ProviderParams struct {
	KeyVaultName string
	Objects      []KeyVaultObject
	TenantID     string
	// TODO: can retrieve from K8s secrets?
	AADClientSecret string
	AADClientID     string
}

// KeyVaultObject representing each secret inside a Vault.
type KeyVaultObject struct {
	ObjectName    string `json: "objectName" yaml: "objectName"`
	ObjectAlias   string `json: "objectAlias" yaml: "objectAlias"`
	ObjectType    string `json: "objectType" yaml: "objectType"`
	ObjectVersion string `json: "objectVersion" yaml: "objectVersion"`
}

const (
	parametersField = "paramaters"
)

func main() {
	handle()
	// getObjectVersions()
	_, err := fetchSecretProviderClasses()
	if err != nil {
		log.Errorf("Erorr!! %s", err.Error())
	}
}

func handle() {
	log.Infof("Hello from Reconciler")
}

// func getObjectVersions() ([]ProviderParams, error) {
// 	// query client and get list of SPC

// }

func fetchSecretProviderClasses() ([]ProviderParams, error) {
	// serviceHost := os.GetEnv("KUBERNETES_SERVICE_HOST")
	// servicePort := os.GetEnv("KUBERNETES_SERVICE_PORT")
	apiVersion := "v1alpha1"            // For example
	namespace := "default"              // For example
	resource := "secretproviderclasses" // For example
	httpMethod := http.MethodGet        // For Example
	urlString := fmt.Sprintf("https://kubernetes.default.svc/apis/secrets-store.csi.x-k8s.io/%s/namespaces/%s/%s", apiVersion, namespace, resource)
	u, err := url.Parse(urlString)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(httpMethod, u.String(), nil)
	if err != nil {
		return nil, err
	}

	caToken, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		panic(err) // cannot find token file
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", string(caToken)))

	caCertPool := x509.NewCertPool()
	caCert, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, err
	}
	caCertPool.AppendCertsFromPEM(caCert)

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
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		log.Errorf("Fetching %s is Forbidden. Error: %s", resource, err.Error())
	}

	resp_body, _ := ioutil.ReadAll(resp.Body)

	fmt.Println(resp.Status)
	// fmt.Println(string(resp_body))
	spew.Dump(string(resp_body))
	return nil, nil
}
