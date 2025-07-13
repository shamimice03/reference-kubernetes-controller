package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/reference-kubernetes-controller/pkg/controller"
	"github.com/reference-kubernetes-controller/pkg/signals"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	klog.InitFlags(nil)

	home, err := os.UserHomeDir()
	if err != nil {
		klog.Fatalf("Failed to get home directory: %v", err)
	}

	kubeconfig := flag.String("kubeconfig", filepath.Join(home, "/.kube/config"), "location of kubeconfig file")
	flag.Parse()

	// Set up signals so we handle the shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	config, err := buildConfig(*kubeconfig)
	if err != nil {
		klog.Fatalf("Failed to build config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create clientset: %v", err)
	}

	// Single shared factory
	factory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	// Create controllers with workqueues
	podController := controller.NewPodController(clientset, factory)
	deploymentController := controller.NewDeploymentController(clientset, factory)

	// Start all informers
	factory.Start(stopCh)

	// Wait for cache sync
	if !cache.WaitForCacheSync(stopCh,
		podController.HasSynced(),
		deploymentController.HasSynced()) {
		klog.Fatal("Failed to wait for cache sync")
	}

	// Start controllers
	go podController.Run(2, stopCh)        // 2 workers
	go deploymentController.Run(1, stopCh) // 1 worker

	klog.Info("Controllers started")
	<-stopCh
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	// Try kubeconfig file first
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err == nil {
		return config, nil
	}

	// Fall back to in-cluster config
	config, err = rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build config: %v", err)
	}

	return config, nil
}
