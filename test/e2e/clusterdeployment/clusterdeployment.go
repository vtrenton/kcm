// Copyright 2024
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package clusterdeployment

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"time"

	"github.com/a8m/envsubst"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cluster-api/api/v1beta1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	kcmv1 "github.com/K0rdent/kcm/api/v1beta1"
	"github.com/K0rdent/kcm/test/e2e/kubeclient"
	"github.com/K0rdent/kcm/test/e2e/logs"
	"github.com/K0rdent/kcm/test/e2e/templates"
	"github.com/K0rdent/kcm/test/utils"
)

type ProviderType string

const (
	ProviderCAPI    ProviderType = "cluster-api"
	ProviderAWS     ProviderType = "infrastructure-aws"
	ProviderAzure   ProviderType = "infrastructure-azure"
	ProviderGCP     ProviderType = "infrastructure-gcp"
	ProviderVSphere ProviderType = "infrastructure-vsphere"
	ProviderAdopted ProviderType = "infrastructure-internal"
)

//go:embed resources/aws-standalone-cp.yaml.tpl
var awsStandaloneCPClusterDeploymentTemplateBytes []byte

//go:embed resources/aws-hosted-cp.yaml.tpl
var awsHostedCPClusterDeploymentTemplateBytes []byte

//go:embed resources/aws-eks.yaml.tpl
var awsEksClusterDeploymentTemplateBytes []byte

//go:embed resources/azure-standalone-cp.yaml.tpl
var azureStandaloneCPClusterDeploymentTemplateBytes []byte

//go:embed resources/azure-hosted-cp.yaml.tpl
var azureHostedCPClusterDeploymentTemplateBytes []byte

//go:embed resources/azure-aks.yaml.tpl
var azureAksClusterDeploymentTemplateBytes []byte

//go:embed resources/gcp-standalone-cp.yaml.tpl
var gcpStandaloneCPClusterDeploymentTemplateBytes []byte

//go:embed resources/gcp-hosted-cp.yaml.tpl
var gcpHostedCPClusterDeploymentTemplateBytes []byte

//go:embed resources/gcp-gke.yaml.tpl
var gcpGkeClusterDeploymentTemplateBytes []byte

//go:embed resources/vsphere-standalone-cp.yaml.tpl
var vsphereStandaloneCPClusterDeploymentTemplateBytes []byte

//go:embed resources/vsphere-hosted-cp.yaml.tpl
var vsphereHostedCPClusterDeploymentTemplateBytes []byte

//go:embed resources/adopted-cluster.yaml.tpl
var adoptedClusterDeploymentTemplateBytes []byte

//go:embed resources/remote-cluster.yaml.tpl
var remoteClusterDeploymentTemplateBytes []byte

func FilterAllProviders() []string {
	return []string{
		utils.KCMControllerLabel,
		GetProviderLabel(ProviderAWS),
		GetProviderLabel(ProviderAzure),
		GetProviderLabel(ProviderCAPI),
		GetProviderLabel(ProviderVSphere),
	}
}

func GetProviderLabel(provider ProviderType) string {
	return fmt.Sprintf("%s=%s", v1beta1.ProviderNameLabel, provider)
}

func GenerateClusterName(postfix string) string {
	mcPrefix := os.Getenv(EnvVarClusterDeploymentPrefix)
	if mcPrefix == "" {
		mcPrefix = "e2e-test-" + uuid.New().String()[:8]
	}

	if postfix != "" {
		return fmt.Sprintf("%s-%s", mcPrefix, postfix)
	}
	return mcPrefix
}

func setClusterName(name string) {
	GinkgoT().Setenv(EnvVarClusterDeploymentName, name)
}

func setTemplate(templateName string) {
	GinkgoT().Setenv(EnvVarClusterDeploymentTemplate, templateName)
}

func Generate(templateType templates.Type, clusterName, template string) *kcmv1.ClusterDeployment {
	GinkgoHelper()

	setClusterName(clusterName)
	setTemplate(template)

	var clusterDeploymentTemplateBytes []byte
	switch templates.GetType(template) {
	case templates.TemplateAWSStandaloneCP:
		clusterDeploymentTemplateBytes = awsStandaloneCPClusterDeploymentTemplateBytes
	case templates.TemplateAWSHostedCP:
		// Validate environment vars that do not have defaults are populated.
		// We perform this validation here instead of within a Before block
		// since we populate the vars from standalone prior to this step.
		ValidateDeploymentVars([]string{
			EnvVarAWSVPCID,
			EnvVarAWSSubnets,
			EnvVarAWSSecurityGroupID,
		})
		clusterDeploymentTemplateBytes = awsHostedCPClusterDeploymentTemplateBytes
	case templates.TemplateAWSEKS:
		clusterDeploymentTemplateBytes = awsEksClusterDeploymentTemplateBytes
	case templates.TemplateVSphereStandaloneCP:
		clusterDeploymentTemplateBytes = vsphereStandaloneCPClusterDeploymentTemplateBytes
	case templates.TemplateVSphereHostedCP:
		// "Generated" prior to HCP cluster deployment
		ValidateDeploymentVars([]string{
			EnvVarVSphereHostedControlPlaneEndpoint,
		})
		clusterDeploymentTemplateBytes = vsphereHostedCPClusterDeploymentTemplateBytes
	case templates.TemplateAzureHostedCP:
		clusterDeploymentTemplateBytes = azureHostedCPClusterDeploymentTemplateBytes
	case templates.TemplateAzureStandaloneCP:
		clusterDeploymentTemplateBytes = azureStandaloneCPClusterDeploymentTemplateBytes
	case templates.TemplateAzureAKS:
		clusterDeploymentTemplateBytes = azureAksClusterDeploymentTemplateBytes
	case templates.TemplateGCPHostedCP:
		clusterDeploymentTemplateBytes = gcpHostedCPClusterDeploymentTemplateBytes
	case templates.TemplateGCPStandaloneCP:
		clusterDeploymentTemplateBytes = gcpStandaloneCPClusterDeploymentTemplateBytes
	case templates.TemplateGCPGKE:
		clusterDeploymentTemplateBytes = gcpGkeClusterDeploymentTemplateBytes
	case templates.TemplateAdoptedCluster:
		clusterDeploymentTemplateBytes = adoptedClusterDeploymentTemplateBytes
	case templates.TemplateRemoteCluster:
		clusterDeploymentTemplateBytes = remoteClusterDeploymentTemplateBytes
	default:
		Fail(fmt.Sprintf("Unsupported template type: %s", templateType))
	}

	clusterDeploymentBytes, err := envsubst.Bytes(clusterDeploymentTemplateBytes)
	Expect(err).NotTo(HaveOccurred(), "failed to substitute environment variables")

	clusterDeployment := &kcmv1.ClusterDeployment{}

	err = yaml.Unmarshal(clusterDeploymentBytes, &clusterDeployment)
	Expect(err).NotTo(HaveOccurred(), "failed to unmarshal ClusterDeployment")
	return clusterDeployment
}

// Create creates a clusterdeployment.k0rdent.mirantis.com and returns a DeleteFunc to clean up the deployment.
// The DeleteFunc is a no-op if the deployment has already been deleted.
func Create(ctx context.Context, cl crclient.Client, clusterDeployment *kcmv1.ClusterDeployment) func() error {
	GinkgoHelper()

	Eventually(func() error {
		err := crclient.IgnoreAlreadyExists(cl.Create(ctx, clusterDeployment))
		if err != nil {
			logs.Println("failed to create ClusterDeployment: " + err.Error())
		}
		return err
	}, 1*time.Minute, 10*time.Second).Should(Succeed())

	return func() error {
		if err := cl.Delete(ctx, clusterDeployment); crclient.IgnoreNotFound(err) != nil {
			return err
		}
		Eventually(func() bool {
			cld := &kcmv1.ClusterDeployment{}
			err := cl.Get(ctx, crclient.ObjectKeyFromObject(clusterDeployment), cld)
			return apierrors.IsNotFound(err)
		}, 30*time.Minute, 1*time.Minute).Should(BeTrue())
		return nil
	}
}

func ValidateDeploymentVars(v []string) {
	GinkgoHelper()

	for _, envVar := range v {
		Expect(os.Getenv(envVar)).NotTo(BeEmpty(), envVar+" must be set")
	}
}

func ValidateClusterTemplates(ctx context.Context, client *kubeclient.KubeClient) error {
	clusterTemplates, err := client.ListClusterTemplates(ctx)
	if err != nil {
		return fmt.Errorf("failed to list cluster templates: %w", err)
	}

	for _, template := range clusterTemplates {
		valid, found, err := unstructured.NestedBool(template.Object, "status", "valid")
		if err != nil {
			return fmt.Errorf("failed to get valid flag for template %s: %w", template.GetName(), err)
		}

		if !found {
			return fmt.Errorf("valid flag for template %s not found", template.GetName())
		}

		if !valid {
			validationError, validationErrFound, err := unstructured.NestedString(template.Object, "status", "validationError")
			if err != nil {
				return fmt.Errorf("failed to get validationError for template %s: %w", template.GetName(), err)
			}
			errStr := "unknown error"
			if validationErrFound {
				errStr = validationError
			}
			return fmt.Errorf("template %s is still invalid: %s", template.GetName(), errStr)
		}
	}

	return nil
}
