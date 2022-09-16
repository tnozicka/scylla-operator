// Copyright (C) 2022 ScyllaDB

package certrotation

import (
	"time"

	"github.com/scylladb/scylla-operator/pkg/crypto"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/events"
)

type Object struct {
	Namespace string
	Name      string
}

type RotatedObject struct {
	Object
	Validity time.Duration
	Refresh  time.Duration
}

type SigningCASecretConfig struct {
	RotatedObject

	Client   corev1client.SecretsGetter
	Informer corev1informers.SecretInformer
	Lister   corev1listers.SecretLister
}

type CABundleConfigMapConfig struct {
	Object

	Client   corev1client.ConfigMapsGetter
	Informer corev1informers.ConfigMapInformer
	Lister   corev1listers.ConfigMapLister
}

type CertKeySecretConfig struct {
	RotatedObject

	CertCreator crypto.CertCreator

	Client   corev1client.SecretsGetter
	Informer corev1informers.SecretInformer
	Lister   corev1listers.SecretLister
}

type Controller struct {
	name                    string
	signingCASecretConfig   SigningCASecretConfig
	CABundleConfigMapConfig CABundleConfigMapConfig
	CertKeySecretConfig     CertKeySecretConfig
	eventRecorder           events.EventRecorder
}
