package main

import (
	"context"
	"flag"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		leaseLockName      string
		leaseLockNamespace string
		identity           string
	)

	flag.StringVar(&leaseLockName, "lease-name", "", "Name of lease lock")
	flag.StringVar(&leaseLockNamespace, "lease-namespace", "", "Name of lease lock namespace")
	flag.StringVar(&identity, "identity", "", "Identity")

	flag.Parse()

	if leaseLockName == "" {
		klog.Fatal("missing lease-name flag")
	}
	if leaseLockNamespace == "" {
		klog.Fatal("missing lease-namespace flag")
	}
	if identity == "" {
		klog.Fatal("missing identity flag")
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatalf("failed to get kubeconfig")
	}
	client := clientset.NewForConfigOrDie(config)

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseLockName,
			Namespace: leaseLockNamespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: identity,
		},
	}

	leaderConfig := leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   10 * time.Second,
		RenewDeadline:   8 * time.Second,
		RetryPeriod:     1 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnNewLeader: func(currentIdentity string) {
				if err := os.WriteFile("/tmp/k8s-leader", []byte(currentIdentity), 0644); err != nil {
					klog.Fatal("unable to write /tmp/k8s-leader")
				}
			},
		},
	}

	leaderelection.RunOrDie(ctx, leaderConfig)
}
