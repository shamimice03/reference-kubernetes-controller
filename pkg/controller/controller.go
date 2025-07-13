package controller

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// PodController demonstrates workqueue usage with Pod resources
type PodController struct {
	clientset kubernetes.Interface
	podLister corev1listers.PodLister
	podSynced cache.InformerSynced
	workqueue workqueue.TypedRateLimitingInterface[any]
}

// DeploymentController demonstrates workqueue usage with Deployment resources
type DeploymentController struct {
	clientset        kubernetes.Interface
	deploymentLister appsv1listers.DeploymentLister
	deploymentSynced cache.InformerSynced
	workqueue        workqueue.TypedRateLimitingInterface[any]
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

// NewDeploymentController creates a new Deployment controller with workqueue
func NewDeploymentController(clientset kubernetes.Interface, factory informers.SharedInformerFactory) *DeploymentController {
	deploymentInformer := factory.Apps().V1().Deployments()

	controller := &DeploymentController{
		clientset:        clientset,
		deploymentLister: deploymentInformer.Lister(),
		deploymentSynced: deploymentInformer.Informer().HasSynced,
		workqueue: workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[any](),
			workqueue.TypedRateLimitingQueueConfig[any]{Name: "Deployments"},
		),
	}

	// Add event handlers
	deploymentInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(newObj)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
	})

	return controller
}

// HasSynced returns true if the pod controller has synced
func (c *PodController) HasSynced() cache.InformerSynced {
	return c.podSynced
}

// HasSynced returns true if the deployment controller has synced
func (c *DeploymentController) HasSynced() cache.InformerSynced {
	return c.deploymentSynced
}

// Run starts the Pod controller
func (c *PodController) Run(workers int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting Pod controller")
	defer klog.Info("Shutting down Pod controller")

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

// Run starts the Deployment controller
func (c *DeploymentController) Run(workers int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting Deployment controller")
	defer klog.Info("Shutting down Deployment controller")

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

// runWorker processes work items from the queue
func (c *DeploymentController) runWorker() {
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

// processNextWorkItem processes a single work item from the queue
func (c *DeploymentController) processNextWorkItem() bool {
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

		if err := c.processDeployment(key); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error processing deployment %s: %w", key, err)
		}

		c.workqueue.Forget(key)
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

	// Your business logic here
	fmt.Printf("[PodController] Processing Pod: %s/%s (Phase: %s)\n",
		pod.Namespace, pod.Name, pod.Status.Phase)

	// Example: Check if pod is in a specific state and take action
	if pod.Status.Phase == corev1.PodPending {
		fmt.Printf("[PodController] Pod %s is pending - might need intervention\n", pod.Name)
	}

	return nil
}

// processDeployment contains your actual business logic for handling Deployment changes
func (c *DeploymentController) processDeployment(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid resource key: %s", key)
	}

	deployment, err := c.deploymentLister.Deployments(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("[DeploymentController] Deployment deleted: %s/%s\n", namespace, name)
			return nil
		}
		return err
	}

	// Your business logic here
	fmt.Printf("[DeploymentController] Processing Deployment: %s/%s (Replicas: %d/%d)\n",
		deployment.Namespace, deployment.Name,
		deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)

	// Example: Check deployment health
	if deployment.Status.ReadyReplicas < *deployment.Spec.Replicas {
		fmt.Printf("[DeploymentController] Deployment %s is not fully ready\n", deployment.Name)
	}

	return nil
}
