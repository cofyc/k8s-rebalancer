package rebalancer

import (
	"reflect"
	"sort"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSortByPriority(t *testing.T) {
	priority1 := int32(1)
	priority2 := int32(2)
	priority3 := int32(3)
	pod1 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-1",
		},
		Spec: v1.PodSpec{
			Priority: &priority1,
		},
	}
	pod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-2",
		},
		Spec: v1.PodSpec{
			Priority: &priority2,
		},
	}
	pod3 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod-3",
		},
		Spec: v1.PodSpec{
			Priority: &priority3,
		},
	}

	testcases := []struct {
		name     string
		pods     []*v1.Pod
		expected []*v1.Pod
	}{
		{
			name:     "sort-ordered",
			pods:     []*v1.Pod{pod1, pod2, pod3},
			expected: []*v1.Pod{pod1, pod2, pod3},
		},
		{
			name:     "sort-unordered",
			pods:     []*v1.Pod{pod2, pod1, pod3},
			expected: []*v1.Pod{pod1, pod2, pod3},
		},
	}

	for _, v := range testcases {
		t.Run(v.name, func(t *testing.T) {
			t.Parallel()
			nodePodPairs := make([]*nodePodPair, 0)
			for _, pod := range v.pods {
				nodePodPairs = append(nodePodPairs, &nodePodPair{
					pod: pod,
				})
			}
			sort.Sort(sortByPriority(nodePodPairs))
			resultPods := make([]*v1.Pod, 0)
			for _, p := range nodePodPairs {
				resultPods = append(resultPods, p.pod)
			}
			if !reflect.DeepEqual(resultPods, v.expected) {
				t.Errorf("expected %v, got %v", v.expected, v.pods)
			}
		})
	}
}
