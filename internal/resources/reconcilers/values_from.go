package reconcilers

import (
	"context"
	"encoding/json"

	"github.com/tidwall/gjson"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcesv1alpha1 "github.com/SigNoz/signoz-operator/api/resources/v1alpha1"
	"github.com/SigNoz/signoz-operator/internal/errors"
	"github.com/SigNoz/signoz-operator/internal/jsonbody"
	"github.com/SigNoz/signoz-operator/internal/resources"
)

// objectWithValues is an Object whose body carries the values spec.valuesFrom
// resolved to. The values live only here: the custom resource is never
// modified, so no write of it can persist them. Identity is the template's.
type objectWithValues struct {
	resources.Object

	values []resolvedValue
}

type resolvedValue struct {
	path  string
	value string
}

func (o *objectWithValues) Body() (json.RawMessage, error) {
	body, err := o.Object.Body()
	if err != nil {
		return nil, err
	}

	for _, resolved := range o.values {
		body, err = jsonbody.Set(body, resolved.path, resolved.value)
		if err != nil {
			return nil, err
		}
	}

	return body, nil
}

func (o *objectWithValues) Hash() (string, error) {
	body, err := o.Body()
	if err != nil {
		return "", err
	}

	return jsonbody.Hash(body)
}

func (o *objectWithValues) ToUpdate() (json.RawMessage, error) {
	body, err := o.Body()
	if err != nil {
		return nil, err
	}

	return jsonbody.ExtractFields(body, o.UpdatableFields())
}

// Compare mirrors the generated objects, which read the remote object from the
// response's data envelope.
func (o *objectWithValues) Compare(response json.RawMessage) (resources.CompareResult, error) {
	body, err := o.Body()
	if err != nil {
		return resources.CompareResult{}, err
	}

	paths := jsonbody.Diff(gjson.ParseBytes(body).Value(), gjson.GetBytes(response, "data").Value())

	return resources.CompareResult{
		UpdatableFields: jsonbody.Intersect(paths, o.UpdatableFields()),
		ImmutableFields: jsonbody.Intersect(paths, o.ImmutableFields()),
	}, nil
}

// withValuesFrom resolves spec.valuesFrom in the object's own namespace. No
// error carries a resolved value.
func withValuesFrom(ctx context.Context, reader client.Reader, obj resources.Object) (resources.Object, error) {
	valuesFrom := obj.GetCoreSpec().ValuesFrom
	if len(valuesFrom) == 0 {
		return obj, nil
	}

	namespace := obj.K8sObject().GetNamespace()
	values := make([]resolvedValue, 0, len(valuesFrom))

	for _, valueFrom := range valuesFrom {
		value, err := resolveValueSource(ctx, reader, namespace, valueFrom.ValueFrom)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ReasonForError(err), "spec.valuesFrom path %q", valueFrom.Path)
		}

		values = append(values, resolvedValue{path: valueFrom.Path, value: value})
	}

	return &objectWithValues{Object: obj, values: values}, nil
}

func resolveValueSource(ctx context.Context, reader client.Reader, namespace string, source resourcesv1alpha1.ValueSource) (string, error) {
	switch {
	case source.SecretKeyRef != nil:
		ref := source.SecretKeyRef

		secret := &corev1.Secret{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, secret); err != nil {
			if apierrors.IsNotFound(err) {
				return "", errors.Newf(errors.ReasonNotFound, "Secret %q not found in namespace %q", ref.Name, namespace)
			}

			return "", errors.Newf(errors.ReasonInternal, "could not read Secret %q in namespace %q: %s", ref.Name, namespace, err)
		}

		value, ok := secret.Data[ref.Key]
		if !ok {
			return "", errors.Newf(errors.ReasonNotFound, "Secret %q in namespace %q has no key %q", ref.Name, namespace, ref.Key)
		}

		return string(value), nil
	case source.ConfigMapKeyRef != nil:
		ref := source.ConfigMapKeyRef

		configMap := &corev1.ConfigMap{}
		if err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: ref.Name}, configMap); err != nil {
			if apierrors.IsNotFound(err) {
				return "", errors.Newf(errors.ReasonNotFound, "ConfigMap %q not found in namespace %q", ref.Name, namespace)
			}

			return "", errors.Newf(errors.ReasonInternal, "could not read ConfigMap %q in namespace %q: %s", ref.Name, namespace, err)
		}

		value, ok := configMap.Data[ref.Key]
		if !ok {
			return "", errors.Newf(errors.ReasonNotFound, "ConfigMap %q in namespace %q has no key %q", ref.Name, namespace, ref.Key)
		}

		return value, nil
	}

	return "", errors.New(errors.ReasonInvalidInput, "valueFrom names neither a secretKeyRef nor a configMapKeyRef")
}
