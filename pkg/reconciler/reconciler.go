// create/update objectVersions with mounted file content
// add versions info to secretProviderClass obj's versions status field

package reconciler

import (
	secretsstore "sigs.k8s.io/secrets-store-csi-driver/pkg/secrets-store"
)

func fun() {
	secretsstore.GetStatus()
}
