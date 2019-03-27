package rebalancer

import (
	"fmt"
	"math"
	"sort"
	"time"

	rebalancercache "github.com/cofyc/k8s-rebalancer/pkg/rebalancer/cache"
	rebalancereviction "github.com/cofyc/k8s-rebalancer/pkg/rebalancer/eviction"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/kubectl/util/qos"
)

const (
	// TODO make these parameters configurable
	rebalanceInterval = time.Second * 5
	// If pods in draining by rebalancer execeeds this threshold, we should
	// skip reblancing and wait them to finish.
	drainingMaxPodsInCluster = 10
	// TODO another way is to compare score between nodes, rebalance if difference of scores is large
	highScoreMark = 7 // inclusive
	targetScore   = 5
	lowScoreMark  = 3 // inclusive
	// If more than maxUnscheduledPodsAllowedInCluster pods are unschedulable
	// in cluster, skip rebalancing and wait for them to be scheduled first.
	maxUnscheduledPodsAllowedInCluster = 3
	drainingTimeout                    = time.Minute

	// Set this annotation in pod if we are go draining it.
	// Note that we clear this annotation if pod DeletionTimestamp is nil,
	// which means we failed to delete/drain this pod last time.
	rebalancerDrainingAnnotation = "k8s-rebalancer.yechenfu.com/draining"
)

var (
	// TODO make it configurable
	gracePeriodSeconds int64 = 120
)

// Rebalancer implements rebalancer controller.
type Rebalancer struct {
	client          kubernetes.Interface
	informerFactory informers.SharedInformerFactory
	nodeLister      corev1listers.NodeLister
	nodesSynced     cache.InformerSynced
	podLister       corev1listers.PodLister
	podsSynced      cache.InformerSynced
	cache           rebalancercache.Interface
}

// assignedPod selects pods that are assigned.
func assignedPod(pod *v1.Pod) bool {
	return len(pod.Spec.NodeName) != 0
}

// terminatedPod checks if pod is already terminated
func terminatedPod(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed
}

// NewRebalancer creates a rebalancer instance.
func NewRebalancer(client kubernetes.Interface, informerFactory informers.SharedInformerFactory) (*Rebalancer, error) {
	nodeInformer := informerFactory.Core().V1().Nodes()
	podInformer := informerFactory.Core().V1().Pods()
	r := &Rebalancer{
		client:      client,
		nodeLister:  nodeInformer.Lister(),
		nodesSynced: nodeInformer.Informer().HasSynced,
		podLister:   podInformer.Lister(),
		podsSynced:  podInformer.Informer().HasSynced,
		cache:       rebalancercache.NewRebalancerCache(),
	}
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.addNodeToCache,
		UpdateFunc: r.updateNodeInCache,
		DeleteFunc: r.deleteNodeFromCache,
	})
	// We only care about scheduled and non-succeeded and non-failed pods
	// kubernetes scheduler ignores succeeded/failed pods too, see
	// https://github.com/kubernetes/kubernetes/blob/7dfcacd1cfcbdfe74b28f2473fb107e9a47ec905/pkg/scheduler/factory/factory.go#L630
	podInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *v1.Pod:
					return assignedPod(t) && !terminatedPod(t)
				case cache.DeletedFinalStateUnknown:
					if pod, ok := t.Obj.(*v1.Pod); ok {
						return assignedPod(pod) && !terminatedPod(pod)
					}
					utilruntime.HandleError(fmt.Errorf("unable to convert object %T to *v1.Pod in %T", obj, r))
					return false
				default:
					utilruntime.HandleError(fmt.Errorf("unable to handle object in %T: %T", r, obj))
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    r.addPodToCache,
				UpdateFunc: r.updatePodInCache,
				DeleteFunc: r.deletePodFromCache,
			},
		},
	)
	return r, nil
}

func (r *Rebalancer) addNodeToCache(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert to *v1.Node: %v", obj)
		return
	}
	r.cache.AddNode(node)
}

func (r *Rebalancer) updateNodeInCache(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert to *v1.Node: %v", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		klog.Errorf("cannot convert to *v1.Node: %v", newObj)
		return
	}
	r.cache.UpdateNode(oldNode, newNode)
}

func (r *Rebalancer) deleteNodeFromCache(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			klog.Errorf("cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1.Node: %v", t)
		return
	}
	r.cache.DeleteNode(node)
}

func (r *Rebalancer) addPodToCache(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		klog.Errorf("cannot convert to *v1.Pod: %v", obj)
		return
	}
	r.cache.AddPod(pod)
}

func (r *Rebalancer) updatePodInCache(oldObj, newObj interface{}) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok {
		klog.Errorf("cannot convert to *v1.Pod: %v", oldObj)
		return
	}
	newPod, ok := newObj.(*v1.Pod)
	if !ok {
		klog.Errorf("cannot convert to *v1.Pod: %v", newObj)
		return
	}
	r.cache.UpdatePod(oldPod, newPod)
}

func (r *Rebalancer) deletePodFromCache(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			klog.Errorf("cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("cannot convert to *v1.Pod: %v", t)
		return
	}
	r.cache.DeletePod(pod)
}

// Run starts the rebalancer.
func (r *Rebalancer) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	klog.Infof("Staring rebalancer")
	defer klog.Infof("Shutting down rebalancer")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForCacheSync(stopCh, r.nodesSynced, r.podsSynced) {
		utilruntime.HandleError(fmt.Errorf("Unable to sync caches for %s", "rebalancer"))
		return
	}

	go wait.Until(r.run, rebalanceInterval, stopCh)

	<-stopCh
}

type scoredNodeInfo struct {
	nodeInfo *rebalancercache.NodeInfo
	score    int64
}

type sortedScoredNodeInfos []*scoredNodeInfo

func (p sortedScoredNodeInfos) Len() int { return len(p) }

func (p sortedScoredNodeInfos) Less(i, j int) bool {
	return p[i].score-p[j].score > 0
}

func (p sortedScoredNodeInfos) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func isPodUnschedulable(pod *v1.Pod) bool {
	_, cond := podutil.GetPodCondition(&pod.Status, v1.PodScheduled)
	return cond != nil && cond.Status == v1.ConditionFalse && cond.Reason == v1.PodReasonUnschedulable
}

func isInDrainingByRebalancer(pod *v1.Pod) bool {
	if _, ok := pod.Annotations[rebalancerDrainingAnnotation]; ok && pod.DeletionTimestamp != nil {
		return true
	}
	return false
}

func (r *Rebalancer) preflightChecks() (skippped bool, err error) {
	// Check unscheduled and non-unschedulable pods
	pods, err := r.podLister.List(labels.Everything())
	if err != nil {
		return true, err
	}
	unscheduledAndNonUnschedulablePods := make([]*v1.Pod, 0)
	for _, pod := range pods {
		if isPodUnschedulable(pod) {
			continue
		}
		if pod.Spec.NodeName == "" {
			unscheduledAndNonUnschedulablePods = append(unscheduledAndNonUnschedulablePods, pod)
		}
	}
	if len(unscheduledAndNonUnschedulablePods) > maxUnscheduledPodsAllowedInCluster {
		klog.Infof("Found %d unscheduled and non-unschedulable pods in cluster (threshold: %d), skip rebalancing", len(unscheduledAndNonUnschedulablePods), maxUnscheduledPodsAllowedInCluster)
		return true, nil
	}
	klog.Infof("Found %d unscheduled and non-unschedulable pods in cluster (threshold: %d), ok to continue", len(unscheduledAndNonUnschedulablePods), maxUnscheduledPodsAllowedInCluster)
	return false, nil
}

func (r *Rebalancer) cleanup() error {
	pods, err := r.podLister.List(labels.Everything())
	if err != nil {
		return err
	}
	// It is possible we marked a pod to drain but failed to drain because API
	// failure or program crash, etc.
	// It's safe to clear our annotation in main single-threaded loop.
	for _, pod := range pods {
		if _, ok := pod.Annotations[rebalancerDrainingAnnotation]; ok && pod.DeletionTimestamp == nil {
			newPod := pod.DeepCopy()
			delete(newPod.Annotations, rebalancerDrainingAnnotation)
			_, err := r.client.CoreV1().Pods(newPod.Namespace).Update(newPod)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Rebalancer) run() {
	klog.V(1).Infof("Begin rebalancing at %s", time.Now().Format(time.RFC3339))
	defer func() {
		klog.V(1).Infof("End rebalancing at %s", time.Now().Format(time.RFC3339))
	}()

	// Cleanup
	if err := r.cleanup(); err != nil {
		klog.Infof("failed to cleanup: %v", err)
	}

	// Preflight checks
	if skipped, err := r.preflightChecks(); err != nil {
		klog.Infof("failed to run preflight checks: %v", err)
	} else if skipped {
		return
	}

	// Snapshot
	snapshot := r.cache.Snapshot()
	if len(snapshot.Nodes) <= 1 {
		klog.Infof("Only %d nodes found in cluster, skip rebalancing", len(snapshot.Nodes))
		return
	}

	// Evaluating
	scoredNodeInfos := make([]*scoredNodeInfo, 0)
	for _, nodeInfo := range snapshot.Nodes {
		// Skip if node is not ready
		if !isNodeReady(nodeInfo.Node()) {
			klog.Infof("node %s is not ready, skipped", nodeInfo.Node().Name)
			continue
		}
		scoredNodeInfos = append(scoredNodeInfos, &scoredNodeInfo{
			nodeInfo: nodeInfo,
			score:    nodeInfo.Score(),
		})
	}
	sort.Sort(sortedScoredNodeInfos(scoredNodeInfos))

	klog.V(4).Infof("DEBUG: List of sorted nodes")
	for _, scoredNodeInfo := range scoredNodeInfos {
		requestedResource := scoredNodeInfo.nodeInfo.RequestedResource()
		allocatableResource := scoredNodeInfo.nodeInfo.AllocatableResource()
		klog.V(4).Infof("DEBUG: node: %s score: %v", scoredNodeInfo.nodeInfo.Node().Name, scoredNodeInfo.score)
		klog.V(4).Infof("DEBUG:    cpu allocatable %v, requested %v (%d %%)", allocatableResource.MilliCPU, requestedResource.MilliCPU, requestedResource.MilliCPU*100/allocatableResource.MilliCPU)
		klog.V(4).Infof("DEBUG:    mem allocatable %v, requested %v (%d %%)", allocatableResource.Memory, requestedResource.Memory, requestedResource.Memory*100/allocatableResource.Memory)
	}

	drainingPods := make([]*v1.Pod, 0)
	drainablePods := make([]*v1.Pod, 0)
	highScoreNodes := make([]*scoredNodeInfo, 0)
	lowScoreNodes := make([]*scoredNodeInfo, 0)

	for _, scoredNodeInfo := range scoredNodeInfos {
		if scoredNodeInfo.score >= highScoreMark {
			highScoreNodes = append(highScoreNodes, scoredNodeInfo)
		} else if scoredNodeInfo.score <= lowScoreMark {
			lowScoreNodes = append(lowScoreNodes, scoredNodeInfo)
		}
		pods := scoredNodeInfo.nodeInfo.Pods()
		for _, pod := range pods {
			if isInDrainingByRebalancer(pod) {
				drainingPods = append(drainingPods, pod)
			}
		}
	}

	if len(drainingPods) >= drainingMaxPodsInCluster {
		klog.Infof("found %d pods in draining (threshold: %d), skp rebalancing", len(drainingPods), drainingMaxPodsInCluster)
		return
	}
	klog.Infof("found %d pods in draining (threshold: %d), ok to continue", len(drainingPods), drainingMaxPodsInCluster)

	if len(highScoreNodes) == 0 || len(lowScoreNodes) == 0 {
		klog.Infof("high score nodes %d found, low score nodes %d found, skip rebalancing", len(highScoreNodes), len(lowScoreNodes))
		return
	}

	podsInHighScoreNode := make([]*nodePodPair, 0)
	for _, highScoreNode := range highScoreNodes {
		pods := highScoreNode.nodeInfo.Pods()
		for _, pod := range pods {
			podsInHighScoreNode = append(podsInHighScoreNode, &nodePodPair{
				nodeInfo: highScoreNode.nodeInfo,
				pod:      pod,
			})
		}
	}

	// sort by priority
	// TODO drain high resource usage pods first?
	sort.Sort(sortByPriority(podsInHighScoreNode))

	for _, nodePodPair := range podsInHighScoreNode {
		pod := nodePodPair.pod
		nodeInfo := nodePodPair.nodeInfo
		// Skip if node reaches target score, we should not drain from this node anymore
		klog.V(4).Infof("score: %d, pod: %s\n", nodeInfo.Score(), pod.Name)
		if nodeInfo.Score() <= targetScore {
			klog.V(4).Infof("skip node %s because its score %d reaches target score %d", nodeInfo.Node().Name, nodeInfo.Score(), targetScore)
			continue
		}
		// Skip BestEffort pods which is useless for our purpose
		if qosClass := qos.GetPodQOS(pod); qosClass == v1.PodQOSBestEffort {
			klog.V(4).Infof("skip pod %s/%s because it is %v QOS class, useless to drain", pod.Namespace, pod.Name, qosClass)
			continue
		}
		// For simplicity, in current version, we ignore pods using PVCs, because
		// PV may have node affinity.
		// TODO support draining pods using PVCs
		if podHasPVCs(pod) {
			klog.V(4).Infof("skip pod %s/%s because it references PVC which may cannot be attached to low score nodes because PV node affinity", pod.Namespace, pod.Name)
			continue
		}
		if pod.Spec.Affinity != nil {
			klog.V(4).Infof("skip pod %s/%s because we do not support rebalancing pod with affinity for now", pod.Namespace, pod.Name)
			continue
		}
		if pod.Spec.NodeSelector != nil {
			klog.V(4).Infof("skip pod %s/%s because we do not support rebalancing pod with node selector for now", pod.Namespace, pod.Name)
			continue
		}
		// Pod to drain must be able to be recreated
		if !r.isAbleToBeRecreatedByController(pod, drainablePods, drainingPods) {
			klog.V(4).Infof("skip pod %s/%s because it seems not able to be recreated", pod.Namespace, pod.Name)
			continue
		}
		// Pod must can be scheduled onto low score nodes
		schedulableNode, err := r.findScheduableNodeFromList(pod, lowScoreNodes)
		if err != nil {
			klog.V(4).Infof("pod %s/%s cannot be scheduled to low score nodes, skipped: %v", pod.Namespace, pod.Name, err)
			continue
		}
		// Mark pod drainable, and remove it from nodeInfo
		if err := nodeInfo.RemovePod(pod); err != nil {
			klog.Errorf("failed to remove pod %s/%s from node %s: %s", pod.Namespace, pod.Name, nodeInfo.Node().Name, err)
			continue
		}
		if nodeInfo.Score() < targetScore {
			klog.V(4).Infof("pod %s/%s cannot be drained, score after drained %d, target score %d", pod.Namespace, pod.Name, nodeInfo.Score(), targetScore)
			nodeInfo.AddPod(pod) // add it back
			continue
		}
		schedulableNode.AddPod(pod)
		drainablePods = append(drainablePods, pod)
		// Check max draining pods in cluster
		if len(drainablePods)+len(drainingPods) >= drainingMaxPodsInCluster {
			klog.Infof("found %d drainable pods and %d draining pods (threshold: %d)", len(drainablePods), len(drainingPods), drainingMaxPodsInCluster)
			break
		}
	}

	// Draining
	if len(drainablePods) == 0 {
		klog.Infof("no drainable pods found")
		return
	}
	for i, pod := range drainablePods {
		updatedPod, err := r.markPod(pod)
		if err != nil {
			klog.Errorf("failed to mark pod %s/%s: %v", pod.Namespace, pod.Name, err)
			return
		}
		drainablePods[i] = updatedPod
	}
	if err := r.drainPods(drainablePods); err != nil {
		klog.Errorf("failed to drain pods: %v", err)
	}
	return
}

func isAbleToScheduledTo(pod *v1.Pod, nodeInfo *rebalancercache.NodeInfo) bool {
	nodeInfo.AddPod(pod)
	defer nodeInfo.RemovePod(pod) // we will add it when pod is able to be drained from original node
	if nodeInfo.Score() > targetScore {
		return false
	}
	return true
}

func (r *Rebalancer) findScheduableNodeFromList(pod *v1.Pod, scoredNodeInfos []*scoredNodeInfo) (*rebalancercache.NodeInfo, error) {
	for _, scoredNodeInfo := range scoredNodeInfos {
		nodeInfo := scoredNodeInfo.nodeInfo
		if !podToleratesNodeTaints(pod, nodeInfo.Node()) {
			continue
		}
		if !isAbleToScheduledTo(pod, nodeInfo) {
			continue
		}
		return nodeInfo, nil
	}
	// TODO evaluate more scheduling criteria
	// - pod node selector
	// - pod node affinity
	// - pv node affinity
	return nil, fmt.Errorf("not found")
}

// By default, only pods managed by ReplicaSet/ReplicationController are able
// to be drained.
// TODO able to configure extra controllers
func (r *Rebalancer) isManagedByValidControllers(pod *v1.Pod) (metav1.OwnerReference, bool) {
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Controller == nil || !(*ownerRef.Controller) {
			continue
		}
		if ownerRef.Kind == "ReplicaSet" || ownerRef.Kind == "ReplicationController" {
			return ownerRef, true
		}
	}
	return metav1.OwnerReference{}, false
}

func podHasPVCs(pod *v1.Pod) bool {
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			return true
		}
	}
	return false
}

func (r *Rebalancer) isAbleToBeRecreatedByController(pod *v1.Pod, drainablePods []*v1.Pod, drainingPods []*v1.Pod) bool {
	ownerRef, ok := r.isManagedByValidControllers(pod)
	if !ok {
		return false
	}
	// We only drain one pod for each mananging controller
	// TODO able to drain more than one pods in safe way
	if r.isOwnedBySameController(ownerRef.UID, append(drainablePods, drainingPods...)) {
		return false
	}
	return true
}

func (r *Rebalancer) isOwnedBySameController(controllerUID types.UID, pods []*v1.Pod) bool {
	controllerUIDs := make(map[types.UID]bool)
	for _, pod := range pods {
		ownerRef, ok := r.isManagedByValidControllers(pod)
		if !ok {
			klog.Errorf("pod %s/%s is not managed by valid controllers, this is unexpected", pod.Namespace, pod.Name)
			continue
		}
		if _, exists := controllerUIDs[ownerRef.UID]; !exists {
			controllerUIDs[ownerRef.UID] = true
		}
	}
	return controllerUIDs[controllerUID]
}

func (r *Rebalancer) markPod(pod *v1.Pod) (*v1.Pod, error) {
	newPod := pod.DeepCopy()
	if newPod.Annotations == nil {
		newPod.Annotations = make(map[string]string)
	}
	newPod.Annotations[rebalancerDrainingAnnotation] = "true"
	return r.client.CoreV1().Pods(newPod.Namespace).Update(newPod)
}

// works like `kubectl drain`
// call `eviction` API if available, otherwise delete pod instead
func (r *Rebalancer) drainPods(pods []*v1.Pod) error {
	if len(pods) == 0 {
		return nil
	}
	klog.Infof("Try to drain %d pods", len(pods))
	policyGroupVersion, err := rebalancereviction.SupportEviction(r.client)
	if err != nil {
		return err
	}
	klog.Infof("Found policy group version: %s", policyGroupVersion)
	if len(policyGroupVersion) > 0 {
		return r.evictPods(pods, policyGroupVersion)
	}
	return r.deletePods(pods)
}

func (r *Rebalancer) evictPods(pods []*v1.Pod, policyGroupVersion string) error {
	returnCh := make(chan error, 1)

	for _, pod := range pods {
		go func(pod *v1.Pod, returnCh chan error) {
			var err error
			for {
				err = rebalancereviction.EvictPod(r.client, pod, policyGroupVersion, gracePeriodSeconds)
				if err == nil {
					break
				} else if apierrors.IsNotFound(err) {
					returnCh <- nil
					return
				} else if apierrors.IsTooManyRequests(err) {
					klog.Errorf("error when evicting pod %q (will retry after 5s): %v\n", pod.Name, err)
					time.Sleep(5 * time.Second)
				} else {
					returnCh <- fmt.Errorf("error when evicting pod %q: %v", pod.Name, err)
					return
				}
			}
			podArray := []*v1.Pod{pod}
			_, err = r.waitForDelete(podArray, 1*time.Second, time.Duration(math.MaxInt64))
			if err == nil {
				returnCh <- nil
			} else {
				returnCh <- fmt.Errorf("error when waiting for pod %q terminating: %v", pod.Name, err)
			}
		}(pod, returnCh)
	}

	doneCount := 0
	var errors []error

	globalTimeoutCh := time.After(drainingTimeout)
	numPods := len(pods)
	for doneCount < numPods {
		select {
		case err := <-returnCh:
			doneCount++
			if err != nil {
				errors = append(errors, err)
			}
		case <-globalTimeoutCh:
			return fmt.Errorf("Drain did not complete within %v", drainingTimeout)
		}
	}
	return utilerrors.NewAggregate(errors)
}

func (r *Rebalancer) deletePods(pods []*v1.Pod) error {
	for _, pod := range pods {
		err := r.deletePod(pod)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	_, err := r.waitForDelete(pods, 1*time.Second, drainingTimeout)
	return err
}

func (r *Rebalancer) deletePod(pod *v1.Pod) error {
	deleteOptions := &metav1.DeleteOptions{}
	if gracePeriodSeconds >= 0 {
		deleteOptions.GracePeriodSeconds = &gracePeriodSeconds
	}
	return r.client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, deleteOptions)
}

func (r *Rebalancer) waitForDelete(pods []*v1.Pod, interval, timeout time.Duration) ([]*v1.Pod, error) {
	err := wait.PollImmediate(interval, timeout, func() (bool, error) {
		pendingPods := []*v1.Pod{}
		for i, pod := range pods {
			p, err := r.client.CoreV1().Pods(pod.Namespace).Get(pod.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) || (p != nil && p.ObjectMeta.UID != pod.ObjectMeta.UID) {
				klog.Infof("pod %s/%s is deleted successfully", pod.Namespace, pod.Name)
				continue
			} else if err != nil {
				return false, err
			} else {
				pendingPods = append(pendingPods, pods[i])
			}
		}
		pods = pendingPods
		if len(pendingPods) > 0 {
			return false, nil
		}
		return true, nil
	})
	return pods, err
}
