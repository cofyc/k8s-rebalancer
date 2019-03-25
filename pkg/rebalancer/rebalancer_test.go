package rebalancer

import (
	"fmt"
	"testing"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/util/logs"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func controllerOwnRefs(kind, uid string) []metav1.OwnerReference {
	t := true
	return []metav1.OwnerReference{
		{
			Controller: &t,
			Kind:       kind,
			UID:        types.UID(uid),
		},
	}
}

func newPriority(p int32) *int32 {
	return &p
}

func TestRebalance(t *testing.T) {
	logs.InitLogs()
	defer logs.FlushLogs()

	nodes := []*v1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-1",
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1000m"),
					v1.ResourceMemory: resource.MustParse("1000Mi"),
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-2",
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1000m"),
					v1.ResourceMemory: resource.MustParse("1000Mi"),
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node-3",
			},
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("1000m"),
					v1.ResourceMemory: resource.MustParse("1000Mi"),
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		},
	}

	testcases := []struct {
		name              string
		pods              []*v1.Pod
		expectDrainedPods map[string]bool
	}{
		{
			name: "basic",
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "pod-a",
						Namespace:       "foo",
						UID:             types.UID("pod-a"),
						OwnerReferences: controllerOwnRefs("ReplicaSet", "a"),
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("200m"),
										v1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
						Priority: newPriority(10),
						NodeName: "node-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "pod-b",
						Namespace:       "foo",
						UID:             types.UID("pod-b"),
						OwnerReferences: controllerOwnRefs("ReplicaSet", "b"),
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("200m"),
										v1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
						Priority: newPriority(8),
						NodeName: "node-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "pod-c",
						Namespace:       "foo",
						UID:             types.UID("pod-c"),
						OwnerReferences: controllerOwnRefs("ReplicaSet", "c"),
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("200m"),
										v1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
						Priority: newPriority(7),
						NodeName: "node-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "pod-d",
						Namespace:       "foo",
						UID:             types.UID("pod-d"),
						OwnerReferences: controllerOwnRefs("ReplicaSet", "d"),
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("200m"),
										v1.ResourceMemory: resource.MustParse("200Mi"),
									},
								},
							},
						},
						NodeName: "node-1",
					},
				},
			},
			expectDrainedPods: map[string]bool{
				"pod-d": true,
			},
		},
		// TODO more cases
	}

	for _, v := range testcases {
		t.Run(v.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			objs := make([]runtime.Object, 0, len(nodes)+len(v.pods))
			for _, node := range nodes {
				objs = append(objs, node)
			}
			for _, pod := range v.pods {
				objs = append(objs, pod)
			}
			client := fake.NewSimpleClientset(objs...)
			informerFactory := informers.NewSharedInformerFactory(client, 0)
			r, err := NewRebalancer(client, informerFactory)
			if err != nil {
				t.Fatal(err)
			}
			// Start informers after all event listeners are registered.
			informerFactory.Start(stopCh)
			// Wait for the caches to be synced before starting workers
			if !cache.WaitForCacheSync(stopCh, r.nodesSynced, r.podsSynced) {
				utilruntime.HandleError(fmt.Errorf("Unable to sync caches for %s", "rebalancer"))
				return
			}
			r.run()
			// Check
			for name := range v.expectDrainedPods {
				pod, err := client.CoreV1().Pods("foo").Get(name, metav1.GetOptions{})
				if !apierrors.IsNotFound(err) {
					t.Errorf("pod %s/%s is not drained", pod.Namespace, pod.Name)
				}
			}
			close(stopCh)
		})
	}
}
