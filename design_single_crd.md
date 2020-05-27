# Secrets Store Rotation Reconciler Doc

## Assumptions

- Pods have been deployed and associated with SPC CRD
- Secrets have been Volume Mounted
- SPC CRD has Pod Information inside the status field

## Definitions

Pod Information

- Pod UID
- Pod Namespace

Secret Provider Class

- Annotation with which node it belongs to (Node UID)

- Status Field
  - List of Objects
    - Object Name
    - Object Version
    - Object Type
    - Object Alias
  - List of Nodes
    - NodeUID
    - List of Pods
      - PodUID
      - namespace
      - name

Provider

- Target Path
- OPTIONAL Provider Secrets (Credentials for Vault)
  -clientid clientsecret
- Attributes (SPC CRD Parameters field)
- Permissions (640)


## Required Flow (To Make it Work)

On Secrets Store CSI Driver Mount
- Update `status` with current object details & pods running on a given node.
  - Common Functionality across components (Secret Reconciler, Reload Reconciler, Driver)


1. Trigger: Timer configured to fire every X minutes

Build Payload
2. Fetch the SPC Objects from the K8s API (API 1)
  a. Filter by Annotation / Label (Investigate) - NODEUID
3. For Each SPC Object
  a. Parameters << KeyVault Name
  b. Status Field
    c. Parameters << Objects
    d. For Each Node
      e. Node == NodeUID envar
        f. For Each Pod
          g. Fetch Pod (PodName, podNamespace) (API 2) - Batch vs Single ?
            h. for each Volume
              i. if Vol == CSI && driver == secrets-store
                j. targetPath << mountName
            k. if nodePublishSecretRef present
              l. fetch k8s secret (nodePublishSecretRef.Name, namespace) (API 3)
                m. Secrets <<  [clientsecret, clientid]
            4. Payload{Secrets, TargetPath, Attributes, Permissions} to Provider Binary
            <!-- Call Provider - WORK IN AZURE PROVIDER -->
            5. Fetch Key Vault
            6. Compare Object Versions (Running vs. Newly Fetched From Vault)
              a. For Each Object
                b. If Object Version Defined -> Skip
                c. If (Newly Fetched Version != Running Version)
                  d. UpdatedObjectsStruct << Newly Fetched Object
                  e. UpdatedObjects written to disk
              f. UpdatedObjectsStruct streamed to StdOut

Update SPC with Updated Objects - Still inside iteration of each SPC
  7. Reconciler awaits until feedback is given on StdOut from the Provider, and then Receives UpdatedObjectsStruct
  8. Reconciler Updates Status Field SPC (4 API) - how does it know which SPC to Update?
    - The reconciler pushes changes to current iteration of
    - ! Hopefully SPC hasn't changed...
  9. EVENTS - MORE THOUGHT. At deployment lvl or all pods?
    10. Reconciler Emits Event to notify Application Operator that a secret has been rotated in the cluster.
11. Next iteration in loop of `SecretProviderClasses`


The secrets store reconciler fetches a list of `SecretProviderClasses` and filters for only those with an annotation that has the same NodeUID as the reconciler has.

```yaml
apiVersion: secrets-store.csi.x-k8s.io/v1alpha1
kind: SecretProviderClass
metadata:
  name: database-secrets
  annotations:
    Nodes:
      - 665478c7-f0d7-4bda-91a7-7a15ad9c2a27
      - 5435adf-f0d7-4bda-91a7-45376765ffa45
spec:
  provider: azure
```

The Reconciler will then loop through the `SecretProviderClasses` Status Fields to pull the current running objects in the cluster, and the pods that are on that cluster utilizing the `SPC`. To make this more streamlined, the `Status Field` will have the objects that are updatable (Those that do not have a specific verison defined at time of deployment) and a list of nodes which each node element containing a list of pods on that node using this `SecretProviderClass`.

```yaml
  parameters:
    keyvaultName: kvrotation-rg-HpdUdBGO2d
    objects: |
      array:
      - |
        objectName: databasePassword
        objectAlias: DATABASE_PASSWORD
        objectType: secret
        objectVersion: ""
      - |
        objectName: storageToken
        objectAlias: STORAGE_TOKEN
        objectType: secret
        objectVersion: d7862daed29f4f60851cbb8af497790c
    tenantId: 72f988bf-86f1-41af-91ab-2d7cd011db47
    usePodIdentity: "false"
  provider: azure
  secretObjects:
  - data:
    - key: databasepassword
      objectName: DATABASE_PASSWORD
    - key: storagetoken
      objectName: STORAGE_TOKEN
    secretName: datbase-creds
    type: Opaque
status:
  objects:
  - objectName: databasePassword
    objectAlias: DATABASE_PASSWORD
    objectVersion: 4fa9626a4aeb4ed19d3b1e4b011655a9
    objectType: secret
  Nodes:
  - NodeUID: 665478c7-f0d7-4bda-91a7-7a15ad9c2a27
    Pods:
    - id: 3e2b27f1-b9dd-49a4-81c5-da6ae48ca496
      name: nginx-secrets-store-inline
      namespace: default
```

While in the loop for each `SecretProviderClass`, we loop through all of the nodes taken from the `Status` field and find the one that matches the `NodeUID` stored in the Reconciler.

Now another loop through each Pod to receive the `mountName` of the secrets-store-driver. The `mountName` is needed to build the `TargetPath` for the payload to the Provider. To Access the `mountName` we'll have to loop again through the Volumes listed on the pod. Once we receive the `mountName` from the correct CSI mount and Driver, we can build out the Targetpath with the `podUID` and `mountName`.

> For the Provider to Execute the TargetPath, the Reconciler Container MUST be a privileged container and have a volumeMount of the Mount Directory containing the pods' volumes that are on the same Node.

The next part of the payload needed are the secrets for authenticating to a given Vault. From the pod, we check if the `nodeSecretPublishRef` is defined, and if so we will use this to find the correct Kubernetes Secret on the cluster that holds the credentials to the Vault.

  ```golang
    nodeUID := env.NodeUID
    payload := Payload {
      Parameters: Parameters{},
      Secrets: Secrets{},
      TargetPath: "/var/lib/kubelet/pods/podUID/volumes/kubernetes~csi.io/mountName/mount",
      Permission: "420"
    }
    for _, spc := range SecretProviderClasses {
      payload.Parameters := SPCParams
      for _, node := range Nodes {
        if node.UID != nodeUID {
          continue
        }

        for _, pod := range node.pods {
          for _, vol := pod.Volumes {
            if vol.CSI == nil { continue }

            if vol.CSI.Driver == "kubernetes~csi.io" {
              mountName := vol.Name
              payload.TargetPath := fmt.Sprintf("/var/lib/kubelet/pods/%s/volumes/kubernetes~csi.io/%s/mount", pod.UID, mountName)
            }
          }
          if pod.nodeSecretPublishRef != nil {
            secretsName := pod.nodeSecretPublishRef.Name
          }
          k8sSecrets := fetchK8sScrets(secretsName)

          payload.Secrets = k8sSecrets
        }
      }
    }
  ```

Now passing the Payload to the Provider, we wait on the response from the Provider that will be streamed to `stdOut`.

> Work will need to be done in the Provider for comparing the Current Running Objects vs the Newly Fetched ones from the Vault.

With the new batch of objects, we will run an Update(Patch/Post) Request to the current `SecretProviderClass` API and its `Status` field with the updated secrets.

The reconciler will then emit an event for an Application Operator, or Cluster Operator to act upon.

The Reconciler moves to the next `SecretProviderClass` object

### Risks

- Reconciler as Privileged Container - Access to the MountPoint Dir to build TargetPath from each pod.
- SPC Could push ETCD storage capacity limits (1 MB).
  - Status Field Growing Drastically with number of pods to track.
- Pods Tracked in SPC Status Field becoming out of sync with those deployed.
- If SPC Is Revised WHILE updating the SPC Status'

### Cons

- Many API Calls (4) to Pull Data to build Payload for Provider and Update the SPC
- Scalability Issues with new Providers.
  - Credentials for Authenticating to a given Vault may differ in where to receive.
  - Take into consideration when building the Payload(Secrets) for the Provider.
- Issue if Pod is rotated / Removed from Node
  - Will need to update the SPC when pod is removed/rotated
  - running updates to SPC to maintain correct Pods tracked in Status
- 2 Separate Functionalities
  - 1 Interval for Calling the Provider (4 API Calls)
  - 1 Interval for Comparing Pods in Node vs those tracked in SPC. (1 API Call)
- N Amount of Pods calling the Provider = N Provider Calls.
  - If 100s of pods deployed and utilizing the Secrets Store. This can be a very costly operation and cause lots of Traffic.
- When Receiving stream of updatedObjects back from Provider, How does the reconciler know which

### Pros

- Container Sidecar decoupled from Secrets Store in same
- Pluggable. Can be swapped out for another Secrets Reconciler.
- No additonal Components Such as extra CRD, Controller.
- Reusable modules for other reconciler (such as the K8s secrets reconciler)

### Observations

- Most Cloud providers provide some type IAM (Identity Access Management) capability
- For On-Prem Clusters `nodeSecretPublishRef` will be used to store credentials on the cluster for connecting to an external Vault
