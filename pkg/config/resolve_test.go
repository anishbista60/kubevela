/*
Copyright 2026 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configv1alpha1 "github.com/oam-dev/kubevela/apis/config.oam.dev/v1alpha1"
	apitypes "github.com/oam-dev/kubevela/apis/types"
)

func fakeResolveClient(objs ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().WithScheme(k8sscheme.Scheme).WithObjects(objs...).Build()
}

func TestResolveConfigTemplate_CRDAvailable(t *testing.T) {
	ct := &configv1alpha1.ConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: "vela-system"},
		Spec: configv1alpha1.ConfigTemplateSpec{
			Template:  "template: {}",
			Scope:     "system",
			Sensitive: true,
		},
		Status: configv1alpha1.ConfigTemplateStatus{Phase: configv1alpha1.ConfigTemplatePhaseAvailable},
	}
	cli := fakeResolveClient(ct)

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "tmpl", Namespace: "vela-system"})
	require.NoError(t, err)
	assert.False(t, waiting)
	require.NotNil(t, tmpl)
	assert.Equal(t, "tmpl", tmpl.Name)
	assert.Equal(t, "vela-system", tmpl.Namespace)
	assert.Equal(t, "system", tmpl.Scope)
	assert.True(t, tmpl.Sensitive)
}

func TestResolveConfigTemplate_CRDNotAvailableIsWaiting(t *testing.T) {
	ct := &configv1alpha1.ConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl", Namespace: "vela-system"},
		Spec:       configv1alpha1.ConfigTemplateSpec{Template: "template: {}"},
		Status:     configv1alpha1.ConfigTemplateStatus{Phase: configv1alpha1.ConfigTemplatePhaseError},
	}
	cli := fakeResolveClient(ct)

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "tmpl", Namespace: "vela-system"})
	require.NoError(t, err)
	assert.True(t, waiting)
	assert.Nil(t, tmpl)
}

func TestResolveConfigTemplate_NeitherCRDNorConfigMapExist(t *testing.T) {
	cli := fakeResolveClient()

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "missing", Namespace: "vela-system"})
	require.ErrorIs(t, err, ErrTemplateNotFound)
	assert.False(t, waiting)
	assert.Nil(t, tmpl)
}

func TestResolveConfigTemplate_FallsBackToLegacyConfigMap(t *testing.T) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TemplateConfigMapNamePrefix + "legacy",
			Namespace: "vela-system",
			Labels:    map[string]string{apitypes.LabelConfigScope: "system"},
			Annotations: map[string]string{
				apitypes.AnnotationConfigSensitive: sensitiveAnnotationValue,
			},
		},
		Data: map[string]string{SaveTemplateKey: "template: {}"},
	}
	cli := fakeResolveClient(cm)

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "legacy", Namespace: "vela-system"})
	require.NoError(t, err)
	assert.False(t, waiting)
	require.NotNil(t, tmpl)
	assert.Equal(t, "legacy", tmpl.Name)
	assert.Equal(t, "vela-system", tmpl.Namespace)
	assert.Equal(t, "system", tmpl.Scope)
	assert.True(t, tmpl.Sensitive)
	assert.Equal(t, "template: {}", string(tmpl.CUE))
}

func TestResolveConfigTemplate_DefaultsEmptyNamespaceToVelaSystem(t *testing.T) {
	ct := &configv1alpha1.ConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "tmpl-default-ns", Namespace: apitypes.DefaultKubeVelaNS},
		Spec:       configv1alpha1.ConfigTemplateSpec{Template: "template: {}"},
		Status:     configv1alpha1.ConfigTemplateStatus{Phase: configv1alpha1.ConfigTemplatePhaseAvailable},
	}
	cli := fakeResolveClient(ct)

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "tmpl-default-ns"})
	require.NoError(t, err)
	assert.False(t, waiting)
	require.NotNil(t, tmpl)
	assert.Equal(t, apitypes.DefaultKubeVelaNS, tmpl.Namespace)
}

func TestResolveConfigTemplate_ConfigTemplateGetErrorPropagates(t *testing.T) {
	base := fakeResolveClient()
	cli := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*configv1alpha1.ConfigTemplate); ok {
				return errors.New("ct boom")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "whatever", Namespace: "vela-system"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ct boom")
	assert.False(t, waiting)
	assert.Nil(t, tmpl)
}

func TestResolveConfigTemplate_ConfigMapGetErrorPropagates(t *testing.T) {
	base := fakeResolveClient()
	cli := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return errors.New("cm boom")
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})

	tmpl, waiting, err := ResolveConfigTemplate(context.TODO(), cli, &configv1alpha1.ConfigTemplateReference{Name: "whatever", Namespace: "vela-system"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cm boom")
	assert.False(t, waiting)
	assert.Nil(t, tmpl)
}
