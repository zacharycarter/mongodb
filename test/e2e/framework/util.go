package framework

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	updateRetryInterval = 10 * 1000 * 1000 * time.Nanosecond
	maxAttempts         = 5
	//TestServiceSuffix   = "-test-svc"
)

func deleteInBackground() *metav1.DeleteOptions {
	policy := metav1.DeletePropagationBackground
	return &metav1.DeleteOptions{PropagationPolicy: &policy}
}

func deleteInForeground() *metav1.DeleteOptions {
	policy := metav1.DeletePropagationForeground
	return &metav1.DeleteOptions{PropagationPolicy: &policy}
}

//func (f *Framework) GetNodePortIP(meta metav1.ObjectMeta) (string, error) {
//	clusterIP := net.IP{192, 168, 99, 100} //minikube ip
//
//	pod, err := f.kubeClient.CoreV1().Pods(meta.Namespace).Get(meta.Name+"-0", metav1.GetOptions{})
//	if err != nil {
//		return "", err
//	}
//
//	if pod.Spec.NodeName != "minikube" {
//		node, err := f.kubeClient.CoreV1().Nodes().Get(pod.Spec.NodeName, metav1.GetOptions{})
//		if err != nil {
//			return "", err
//		}
//
//		for _, addr := range node.Status.Addresses {
//			if addr.Type == core.NodeExternalIP {
//				clusterIP = net.ParseIP(addr.Address)
//				break
//			}
//		}
//	}
//
//	svc, err := f.kubeClient.CoreV1().Services(f.Namespace()).Get(meta.Name+TestServiceSuffix, metav1.GetOptions{})
//	if err != nil {
//		return "", err
//	}
//
//	nodePort := strconv.Itoa(int(svc.Spec.Ports[0].NodePort))
//	address := fmt.Sprintf(clusterIP.String() + ":" + nodePort)
//	return address, nil
//}

//func (i *Invocation) CreateTestService(mongodbMeta metav1.ObjectMeta) error {
//	mongodb, err := i.GetMongoDB(mongodbMeta)
//	if err != nil {
//		return err
//	}
//
//	svcMeta := metav1.ObjectMeta{
//		Name:      mongodbMeta.Name + TestServiceSuffix,
//		Namespace: mongodbMeta.Namespace,
//	}
//
//	ref, rerr := reference.GetReference(clientsetscheme.Scheme, mongodb)
//	if rerr != nil {
//		return rerr
//	}
//
//	_, _, err = core_util.CreateOrPatchService(i.kubeClient, svcMeta, func(in *core.Service) *core.Service {
//		in.ObjectMeta = core_util.EnsureOwnerReference(in.ObjectMeta, ref)
//		in.Labels = mongodb.OffshootLabels()
//		in.Spec.Type = core.ServiceTypeNodePort
//		in.Spec.Ports = core_util.MergeServicePorts(in.Spec.Ports, []core.ServicePort{
//			{
//				Name:       "db-test",
//				Protocol:   core.ProtocolTCP,
//				Port:       27017,
//				NodePort:   32757,
//				TargetPort: intstr.FromString("db"),
//			},
//		})
//		in.Spec.Selector = mongodb.OffshootLabels()
//		return in
//	})
//
//	return err
//}
//
//func (i *Invocation) DeleteTestService(meta metav1.ObjectMeta) error {
//	err := i.kubeClient.CoreV1().Services(meta.Namespace).Delete(meta.Name+TestServiceSuffix, deleteInForeground())
//	if err != nil && !kerr.IsNotFound(err) {
//		return err
//	}
//	return nil
//}
