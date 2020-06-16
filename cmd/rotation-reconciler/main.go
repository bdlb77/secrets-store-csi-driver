package main

import (
	"context"

	"sigs.k8s.io/secrets-store-csi-driver/pkg/rotation_reconciler"
)

func main() {
	// var ctx context.TODO()
	// // interval every X (time.Second)
	// pollInterval := 60

	// timerCh := time.Tick(time.Duration(pollInterval) * time.Second)

	// for range timerCh {
	rotation_reconciler.Reconciler(context.TODO())
	// }

	// it, _ := secretsstore.GetSecretProviderItem(context.TODO(), "azure-spc", "default")
}
