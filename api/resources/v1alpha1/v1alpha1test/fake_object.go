// +kubebuilder:skip
package v1alpha1test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
)

type FakeObject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   v1alpha1.CoreSpec   `json:"spec,omitempty"`
	Status v1alpha1.CoreStatus `json:"status,omitempty"`
}

func (o *FakeObject) DeepCopyObject() runtime.Object {
	out := &FakeObject{TypeMeta: o.TypeMeta}
	o.DeepCopyInto(&out.ObjectMeta)
	o.Spec.DeepCopyInto(&out.Spec)
	o.Status.DeepCopyInto(&out.Status)

	return out
}

func Scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypes(GroupVersion, &FakeObject{})
	metav1.AddToGroupVersion(s, GroupVersion)

	return s
}

var GroupVersion = schema.GroupVersion{
	Group:   "fake.resources.signoz.io",
	Version: "v1alpha1",
}
