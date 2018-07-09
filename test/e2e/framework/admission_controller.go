package framework

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/appscode/go/log"
	shell "github.com/codeskyblue/go-sh"
	"github.com/kubedb/mongodb/pkg/cmds/server"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	kerr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kApi "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

var (
	DockerRegistry     string
	ExporterTag        string
	SelfHostedOperator bool
	DBVersion          string
)

func (f *Framework) isApiSvcReady(apiSvcName string) error {
	apiSvc, err := f.kaClient.ApiregistrationV1beta1().APIServices().Get(apiSvcName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for _, cond := range apiSvc.Status.Conditions {
		if cond.Type == kApi.Available && cond.Status == kApi.ConditionTrue {
			log.Infof("APIService %v status is true", apiSvcName)
			return nil
		}
	}
	log.Errorf("APIService %v not ready yet", apiSvcName)
	return fmt.Errorf("APIService %v not ready yet", apiSvcName)
}

func (f *Framework) EventuallyAPIServiceReady() GomegaAsyncAssertion {
	return Eventually(
		func() error {
			if err := f.isApiSvcReady("v1alpha1.mutators.kubedb.com"); err != nil {
				return err
			}
			if err := f.isApiSvcReady("v1alpha1.validators.kubedb.com"); err != nil {
				return err
			}
			time.Sleep(time.Second * 3) // let the resource become available
			return nil
		},
		time.Minute*2,
		time.Second*5,
	)
}

func (f *Framework) RunOperatorAndServer(kubeconfigPath string, stopCh <-chan struct{}) {
	defer GinkgoRecover()

	sh := shell.NewSession()
	args := []interface{}{"--namespace", f.Namespace()}
	SetupServer := filepath.Join("..", "..", "hack", "dev", "setup.sh")

	By("Creating API server and webhook stuffs")
	cmd := sh.Command(SetupServer, args...)
	err := cmd.Run()
	Expect(err).ShouldNot(HaveOccurred())

	By("Starting Server and Operator")
	serverOpt := server.NewMongoDBServerOptions(os.Stdout, os.Stderr)

	serverOpt.RecommendedOptions.CoreAPI.CoreAPIKubeconfigPath = kubeconfigPath
	serverOpt.RecommendedOptions.SecureServing.BindPort = 8443
	serverOpt.RecommendedOptions.SecureServing.BindAddress = net.ParseIP("127.0.0.1")
	serverOpt.RecommendedOptions.Authorization.RemoteKubeConfigFile = kubeconfigPath
	serverOpt.RecommendedOptions.Authentication.RemoteKubeConfigFile = kubeconfigPath

	serverOpt.ExtraOptions.Docker.Registry = DockerRegistry
	serverOpt.ExtraOptions.Docker.ExporterTag = ExporterTag

	err = serverOpt.Run(stopCh)
	Expect(err).NotTo(HaveOccurred())
}

func (f *Framework) CleanAdmissionConfigs() {
	// delete validating Webhook
	if err := f.kubeClient.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().DeleteCollection(deleteInBackground(), metav1.ListOptions{
		LabelSelector: "app=kubedb",
	}); err != nil && !kerr.IsNotFound(err) {
		fmt.Printf("error in deletion of Validating Webhook. Error: %v", err)
	}

	// delete mutating Webhook
	if err := f.kubeClient.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().DeleteCollection(deleteInBackground(), metav1.ListOptions{
		LabelSelector: "app=kubedb",
	}); err != nil && !kerr.IsNotFound(err) {
		fmt.Printf("error in deletion of Mutating Webhook. Error: %v", err)
	}

	// Delete APIService
	if err := f.kaClient.ApiregistrationV1beta1().APIServices().DeleteCollection(deleteInBackground(), metav1.ListOptions{
		LabelSelector: "app=kubedb",
	}); err != nil && !kerr.IsNotFound(err) {
		fmt.Printf("error in deletion of APIService. Error: %v", err)
	}

	// Delete Service
	if err := f.kubeClient.CoreV1().Services("kube-system").Delete("kubedb-operator", &metav1.DeleteOptions{}); err != nil && !kerr.IsNotFound(err) {
		fmt.Printf("error in deletion of Service. Error: %v", err)
	}

	// Delete EndPoints
	if err := f.kubeClient.CoreV1().Endpoints("kube-system").DeleteCollection(deleteInBackground(), metav1.ListOptions{
		LabelSelector: "app=kubedb",
	}); err != nil && !kerr.IsNotFound(err) {
		fmt.Printf("error in deletion of Endpoints. Error: %v", err)
	}

	time.Sleep(time.Second * 1) // let the kube-server know it!!
}
