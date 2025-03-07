package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		klog.Infof("Received signal: %s", sig)
		cancel()
	}()

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

	var config *rest.Config
	var client *clientset.Clientset
	if os.Getenv("KUBECONFIG") != "" {
		if c, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG")); err != nil {
			klog.Fatalln("failed to read KUBECONFIG")
		} else {
			config = c
		}
	} else {
		if c, err := rest.InClusterConfig(); err != nil {
			klog.Fatalf("failed to get kubeconfig")
		} else {
			config = c
		}
	}
	client = clientset.NewForConfigOrDie(config)

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

	// No idea why I originally added this
	for {
		if existingLock, err := client.CoordinationV1().Leases(leaseLockNamespace).Get(ctx, leaseLockName, metav1.GetOptions{}); err != nil {
			klog.Errorf("Error while checking existing lock: %s", err)
			if errors.IsNotFound(err) {
				klog.Infof("Lease %s not found, proceeding with leader election", leaseLockName)
				break
			}
			klog.Errorf("Error while checking existing lock: %s", err)
		} else if existingLock.Spec.HolderIdentity != nil && *existingLock.Spec.HolderIdentity == identity {
			klog.Errorf("Lock with identity %s already exists", identity)
		} else {
			break
		}

		if ctx.Err() != nil {
			os.Exit(0)
		}

		time.Sleep(3 * time.Second)
	}

	klog.Info("Starting leader election")
	leaderConfig := leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   10 * time.Second,
		RenewDeadline:   8 * time.Second,
		RetryPeriod:     1 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.Info("started leading")
				podName := os.Getenv("HOSTNAME")
				podNamespace := os.Getenv("NAMESPACE")
				if podName == "" || podNamespace == "" {
					klog.Error("HOSTNAME or NAMESPACE environment variable is not set")
					return
				}

				patch := []byte(`{
					"metadata": {
						"labels": {
							"k8s-leader": "yes"
						}
					}
				}`)
				_, err := client.CoreV1().Pods(podNamespace).Patch(ctx, podName, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
				if err != nil {
					klog.Errorf("Failed to label pod: %v", err)
				} else {
					klog.Info("Pod labeled as k8s-leader=yes")
				}
			},
			OnStoppedLeading: func() {
				klog.Info("stopped leading")
			},
			OnNewLeader: func(currentIdentity string) {
				if currentIdentity != identity {
					klog.Infof("Somebody else is leader: %s", currentIdentity)
				} else {
					klog.Infof("I am the leader")
				}

				if err := os.WriteFile("/tmp/k8s-leader", []byte(currentIdentity), 0644); err != nil {
					klog.Error("unable to write /tmp/k8s-leader")
				}
			},
		},
	}

	for {
		leaderelection.RunOrDie(ctx, leaderConfig)
		if ctx.Err() != nil {
			break
		}

		klog.Info("RunOrDie exited without ctx done, retrying")
		time.Sleep(1 * time.Second)
	}

	klog.Info("bye")
}
