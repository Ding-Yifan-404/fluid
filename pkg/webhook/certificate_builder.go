/*
Copyright 2021 The Fluid Authors.

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

package webhook

import (
	"context"
	"fmt"
	"github.com/fluid-cloudnative/fluid/pkg/utils/webhook/generator"
	"github.com/fluid-cloudnative/fluid/pkg/utils/webhook/writer"
	"k8s.io/apimachinery/pkg/types"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "k8s.io/api/admissionregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CertificateBuilder struct {
	log logr.Logger
	client.Client
}

func NewCertificateBuilder(c client.Client, log logr.Logger) *CertificateBuilder {
	ch := &CertificateBuilder{Client: c, log: log}
	return ch
}

// BuildAndSyncCABundle use service name and namespace generate webhook caBundle
// and patch the caBundle to MutatingWebhookConfiguration
func (c *CertificateBuilder) BuildAndSyncCABundle(svcName, webhookName, cerPath string) error {

	ns, err := utils.GetEnvByKey(common.MyPodNamespace)
	if err != nil {
		return errors.Wrapf(err, "get namespace from env failed, env key:%s", common.MyPodNamespace)
	}
	c.log.Info("start generate certificate", "service", svcName, "namespace", ns, "cert dir", cerPath)

	certs, err := c.genCA(ns, svcName, cerPath)
	if err != nil {
		return err
	}

	err = c.PatchCABundle(webhookName, certs.CACert)
	if err != nil {
		return err
	}
	return nil
}

// genCA generate the caBundle and store it in secret and local path
func (c *CertificateBuilder) genCA(ns, svc, certPath string) (*generator.Artifacts, error) {

	certWriter, err := writer.NewSecretCertWriter(writer.SecretCertWriterOptions{
		Client: c.Client,
		Secret: &types.NamespacedName{Namespace: ns, Name: common.CertSecretName},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to new certWriter: %v", err)
	}

	dnsName := generator.ServiceToCommonName(ns, svc)

	certs, _, err := certWriter.EnsureCert(dnsName)
	if err != nil {
		return certs, fmt.Errorf("failed to ensure certs: %v", err)
	}

	if err := writer.WriteCertsToDir(certPath, certs); err != nil {
		return certs, fmt.Errorf("failed to WriteCertsToDir: %v", err)
	}
	return certs, nil
}

// PatchCABundle patch the caBundle to MutatingWebhookConfiguration
func (c *CertificateBuilder) PatchCABundle(webHookName string, ca []byte) error {

	var m v1.MutatingWebhookConfiguration

	c.log.Info("start patch MutatingWebhookConfiguration caBundle", "name", webHookName)

	ctx := context.Background()

	if err := c.Get(ctx, client.ObjectKey{Name: webHookName}, &m); err != nil {
		c.log.Error(err, "fail to get mutatingWebHook", "name", webHookName)
		return err
	}

	current := m.DeepCopy()
	for i := range m.Webhooks {
		m.Webhooks[i].ClientConfig.CABundle = ca
	}

	if err := c.Patch(ctx, &m, client.MergeFrom(current)); err != nil {
		c.log.Error(err, "fail to patch CABundle to mutatingWebHook", "name", webHookName)
		return err
	}

	c.log.Info("finished patch MutatingWebhookConfiguration caBundle", "name", webHookName)

	return nil
}
