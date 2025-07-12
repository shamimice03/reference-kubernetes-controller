package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// PodController demonstrates workqueue usage with pod resources
type PodController struct {
	clientset kubernetes.Interface
	podLister corev1listers.PodLister
	podSynced cache.InformerSynced
	workqueue workqueue.TypedRateLimitingInterface[any]
}

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		klog.Fatalf("Failed to get home directory: %v", err)
	}

	kubeconfig := flag.String("kubeconfig", filepath.Join(home, "/.kube/config"), "location of kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
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
	podController := NewPodController(clientset, factory)

	// Start all informers
	stopCh := make(chan struct{})
	factory.Start(stopCh)

	// Wait for cache sync
	if !cache.WaitForCacheSync(stopCh,
		podController.podSynced) {
		klog.Fatal("Failed to wait for cache sync")
	}

	// Start controllers
	go podController.Run(2, stopCh) // 2 workers

	klog.Info("Controllers started")
	<-stopCh
}

// NewPodController creates a new Pod controller with workqueue
func NewPodController(clientset kubernetes.Interface, factory informers.SharedInformerFactory) *PodController {
	podInformer := factory.Core().V1().Pods()

	controller := &PodController{
		clientset: clientset,
		podLister: podInformer.Lister(),
		podSynced: podInformer.Informer().HasSynced,
		workqueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[any](),
			workqueue.TypedRateLimitingQueueConfig[any]{Name: "Pods"},
		),
	}

	// Add event handlers that enqueue work items
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
				klog.V(4).Infof("Pod added to queue: %s", key)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err == nil {
				controller.workqueue.Add(key)
				klog.V(4).Infof("Pod updated, added to queue: %s", key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
				klog.V(4).Infof("Pod deleted, added to queue: %s", key)
			}
		},
	})

	return controller
}

// Run() → Starts workers → runWorker() → processNextWorkItem() → processPod()
//   ↑                         ↓              ↓                      ↓
// Controller entry point   Infinite loop   Gets from queue    Business logic

// Run starts the Pod controller
func (c *PodController) Run(workers int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()
	defer klog.Info("Shutting down Pod controller")

	klog.Info("Starting Pod controller")

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

// runWorker processes work items from the queue
func (c *PodController) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem processes a single work item from the queue
func (c *PodController) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)

		key, ok := obj.(string)
		if !ok {
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.processPod(key); err != nil {
			// Re-queue with rate limiting if processing failed
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error processing pod %s: %w", key, err)
		}

		// Successful processing - reset rate limiting
		c.workqueue.Forget(key)
		klog.V(4).Infof("Successfully processed pod: %s", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
	}

	return true
}

// processPod contains your actual business logic for handling Pod changes
func (c *PodController) processPod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %s", key)
	}

	// Get the Pod from the lister (cache)
	pod, err := c.podLister.Pods(namespace).Get(name)
	if err != nil {
		// Pod was deleted
		if errors.IsNotFound(err) {
			fmt.Printf("[PodController] Pod deleted: %s/%s\n", namespace, name)
			return nil
		}
		return err
	}

	// our business logic here
	fmt.Printf("[PodController] Processing Pod: %s/%s (Phase: %s)\n",
		pod.Namespace, pod.Name, pod.Status.Phase)

	// Example: Check if pod is in a specific state and take action
	if pod.Status.Phase == corev1.PodPending {
		fmt.Printf("[PodController] Pod %s is pending - might need intervention\n", pod.Name)
	}

	return nil
}
