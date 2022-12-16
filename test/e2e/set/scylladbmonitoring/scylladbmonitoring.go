// Copyright (C) 2022 ScyllaDB

package scylladbmonitoring

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	prometheusappclient "github.com/prometheus/client_golang/api"
	promeheusappv1api "github.com/prometheus/client_golang/api/prometheus/v1"
	opointer "github.com/scylladb/scylla-operator/pkg/pointer"
	scyllafixture "github.com/scylladb/scylla-operator/test/e2e/fixture/scylla"
	"github.com/scylladb/scylla-operator/test/e2e/framework"
	"github.com/scylladb/scylla-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = g.Describe("ScyllaDBMonitoring", func() {
	defer g.GinkgoRecover()

	f := framework.NewFramework("scylladbmonitoring")

	g.It("should setup monitoring stack", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		sc := scyllafixture.BasicScyllaCluster.ReadOrFail()
		o.Expect(sc.Spec.Datacenter.Racks).To(o.HaveLen(1))
		sc.Spec.Datacenter.Racks[0].Members = 1

		framework.By("Creating a ScyllaCluster with a single node")
		sc, err := f.ScyllaClient().ScyllaV1().ScyllaClusters(f.Namespace()).Create(
			ctx,
			sc,
			metav1.CreateOptions{
				FieldManager:    f.FieldManager(),
				FieldValidation: metav1.FieldValidationStrict,
			},
		)
		o.Expect(err).NotTo(o.HaveOccurred())

		// framework.By("Waiting for the ScyllaCluster to rollout (RV=%s)", sc.ResourceVersion)
		// waitCtxL1, waitCtxL1Cancel := utils.ContextForRollout(ctx, sc)
		// defer waitCtxL1Cancel()
		// sc, err = utils.WaitForScyllaClusterState(waitCtxL1, f.ScyllaClient().ScyllaV1(), sc.Namespace, sc.Name, utils.WaitForStateOptions{}, utils.IsScyllaClusterRolledOut)
		// o.Expect(err).NotTo(o.HaveOccurred())

		framework.By("Creating a ScyllaDBMonitoring")
		sm, _, err := scyllafixture.ScyllaDBMonitoringTemplate.RenderObject(map[string]string{
			"name":              sc.Name,
			"namespace":         sc.Namespace,
			"scyllaClusterName": sc.Name,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		sm, err = f.ScyllaClient().ScyllaV1alpha1().ScyllaDBMonitorings(sc.Namespace).Create(
			ctx,
			sm,
			metav1.CreateOptions{
				FieldManager:    f.FieldManager(),
				FieldValidation: metav1.FieldValidationStrict,
			},
		)
		o.Expect(err).NotTo(o.HaveOccurred())

		prometheusE2EIngress, _, err := scyllafixture.ScyllaDBMonitoringE2EPrometheusIngressTemplate.RenderObject(map[string]string{
			"scyllaDBMonitoringName": sm.Name,
			"namespace":              sm.Namespace,
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		prometheusE2EIngress, err = f.KubeClient().NetworkingV1().Ingresses(sm.Namespace).Create(
			ctx,
			prometheusE2EIngress,
			metav1.CreateOptions{
				FieldManager:    f.FieldManager(),
				FieldValidation: metav1.FieldValidationStrict,
			},
		)
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Expect(prometheusE2EIngress.Spec.Rules).To(o.HaveLen(1))
		prometheusServerName := prometheusE2EIngress.Spec.Rules[0].Host

		framework.By("Waiting for the ScyllaDBMonitoring to rollout (RV=%s)", sm.ResourceVersion)
		waitCtx2, waitCtx2Cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer waitCtx2Cancel()
		sm, err = utils.WaitForScyllaDBMonitoringState(waitCtx2, f.ScyllaClient().ScyllaV1alpha1().ScyllaDBMonitorings(sc.Namespace), sc.Name, utils.WaitForStateOptions{}, utils.IsScyllaDBMonitoringRolledOut)
		o.Expect(err).NotTo(o.HaveOccurred())

		framework.By("Verifying that Prometheus is configured correctly", sm.ResourceVersion)

		prometheusServingCABundleConfigMap, err := f.KubeClient().CoreV1().ConfigMaps(f.Namespace()).Get(ctx, fmt.Sprintf("%s-prometheus-serving-ca", sc.Name), metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		prometheusServingCACerts, _ := verifyAndParseCABundle(prometheusServingCABundleConfigMap)
		o.Expect(prometheusServingCACerts).To(o.HaveLen(1))

		prometheusServingCAPool := x509.NewCertPool()
		prometheusServingCAPool.AddCert(prometheusServingCACerts[0])

		grafanaClientSecret, err := f.KubeClient().CoreV1().Secrets(f.Namespace()).Get(ctx, fmt.Sprintf("%s-prometheus-client-grafana", sc.Name), metav1.GetOptions{})
		o.Expect(err).NotTo(o.HaveOccurred())
		_, grafanaClientCertBytes, _, grafanaClientKeyBytes := verifyAndParseTLSCert(grafanaClientSecret, verifyTLSCertOptions{
			isCA:     pointer.Bool(false),
			keyUsage: opointer.KeyUsage(x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature),
		})

		adminTLSCert, err := tls.X509KeyPair(grafanaClientCertBytes, grafanaClientKeyBytes)
		o.Expect(err).NotTo(o.HaveOccurred())

		promHTTPClient, err := prometheusappclient.NewClient(prometheusappclient.Config{
			// FIXME: wire ingress IP flag and send it there
			Address: fmt.Sprintf("https://%s-prometheus.%s.svc", sm.Name, sm.Namespace),
			Client: &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						ServerName:   prometheusServerName,
						Certificates: []tls.Certificate{adminTLSCert},
						RootCAs:      prometheusServingCAPool,
					},
					Proxy: http.ProxyFromEnvironment,
					DialContext: (&net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}).DialContext,
					ForceAttemptHTTP2:     true,
					MaxIdleConns:          100,
					IdleConnTimeout:       90 * time.Second,
					TLSHandshakeTimeout:   10 * time.Second,
					ExpectContinueTimeout: 1 * time.Second,
				},
			},
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		promClient := promeheusappv1api.NewAPI(promHTTPClient)

		ctxTargets, ctxTargetsCancel := context.WithTimeout(ctx, 15*time.Second)
		defer ctxTargetsCancel()
		targets, err := promClient.Targets(ctxTargets)
		o.Expect(err).NotTo(o.HaveOccurred())

		o.Expect(targets.Dropped).To(o.HaveLen(0))

		o.Expect(targets.Active).To(o.HaveLen(1))
		for _, t := range targets.Active {
			o.Expect(t.Health).To(o.Equal(promeheusappv1api.HealthGood))
		}
	})
})
