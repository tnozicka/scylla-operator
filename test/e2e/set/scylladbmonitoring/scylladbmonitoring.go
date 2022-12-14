// Copyright (C) 2022 ScyllaDB

package scylladbmonitoring

import (
	"context"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	scyllafixture "github.com/scylladb/scylla-operator/test/e2e/fixture/scylla"
	"github.com/scylladb/scylla-operator/test/e2e/framework"
	"github.com/scylladb/scylla-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

		framework.By("Waiting for the ScyllaCluster to rollout (RV=%s)", sc.ResourceVersion)
		waitCtxL1, waitCtxL1Cancel := utils.ContextForRollout(ctx, sc)
		defer waitCtxL1Cancel()
		sc, err = utils.WaitForScyllaClusterState(waitCtxL1, f.ScyllaClient().ScyllaV1(), sc.Namespace, sc.Name, utils.WaitForStateOptions{}, utils.IsScyllaClusterRolledOut)
		o.Expect(err).NotTo(o.HaveOccurred())

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

		framework.By("Waiting for the ScyllaDBMonitoring to rollout (RV=%s)", sm.ResourceVersion)
		waitCtx2, waitCtx2Cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer waitCtx2Cancel()
		sm, err = utils.WaitForScyllaDBMonitoringState(waitCtx2, f.ScyllaClient().ScyllaV1alpha1().ScyllaDBMonitorings(sc.Namespace), sc.Name, utils.WaitForStateOptions{}, utils.IsScyllaDBMonitoringRolledOut)
		o.Expect(err).NotTo(o.HaveOccurred())

		framework.By("Waiting for the ScyllaDBMonitoring to rollout (RV=%s)", sm.ResourceVersion)
	})
})
