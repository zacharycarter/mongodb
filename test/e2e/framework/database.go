package framework

import (
	"fmt"
	"time"

	"github.com/appscode/kutil/tools/portforward"
	"github.com/globalsign/mgo/bson"
	"github.com/go-bongo/bongo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Todo: use official go-mongodb driver. https://github.com/mongodb/mongo-go-driver
// Currently in Alpha Release.
//
// Connect to each replica set instances to check data.
// Currently `Secondary Nodes` not supported in used drivers.

type KubedbTable struct {
	bongo.DocumentBase `bson:",inline"`
	FirstName          string
	LastName           string
}

func (f *Framework) GetMongoDBClient(meta metav1.ObjectMeta, dbName string, clientPodName string) (*bongo.Connection, error) {
	mongodb, err := f.GetMongoDB(meta)
	if err != nil {
		return nil, err
	}
	tunnel := portforward.NewTunnel(
		f.kubeClient.CoreV1().RESTClient(),
		f.restConfig,
		mongodb.Namespace,
		clientPodName,
		27017,
	)

	if err := tunnel.ForwardPort(); err != nil {
		return nil, err
	}
	user := "root"
	pass, err := f.GetMongoDBRootPassword(mongodb)

	connectionUrl := fmt.Sprintf("mongodb://%s:%s@127.0.0.1:%v", user, pass, tunnel.Local)

	if mongodb.Spec.ClusterMode != nil &&
		mongodb.Spec.ClusterMode.ReplicaSet != nil {
		connectionUrl += fmt.Sprintf("/?replicaSet=%v", mongodb.Spec.ClusterMode.ReplicaSet.Name)
	}

	//info := mgo.DialInfo{
	//	Addrs:          []string{"localhost:40012"},
	//	Timeout:        5 * time.Second,
	//	ReplicaSetName: "rs1",
	//}

	fmt.Println(">>>>>>>>>>>>>>>>>>>>>>>>>>>", connectionUrl)

	config := &bongo.Config{
		ConnectionString: connectionUrl,
		Database:         dbName,
	}

	return bongo.Connect(config)

}

func (f *Framework) EventuallyInsertDocument(meta metav1.ObjectMeta, dbName string, clientPodName string) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			en, err := f.GetMongoDBClient(meta, dbName, clientPodName)
			if err != nil {
				fmt.Println("GetMongoDB Client error", err)
				return false
			}

			defer en.Session.Close()

			if err := en.Session.Ping(); err != nil {
				fmt.Println("Ping error", err)
				return false
			}

			person := &KubedbTable{
				FirstName: "kubernetes",
				LastName:  "database",
			}

			if err := en.Collection("people").Save(person); err != nil {
				fmt.Println("creation error", err)
				return false
			}
			return true
		},
		time.Minute*5,
		time.Second*5,
	)
}

func (f *Framework) EventuallyDocumentExists(meta metav1.ObjectMeta, dbName string, clientPodName string) GomegaAsyncAssertion {
	return Eventually(
		func() bool {
			en, err := f.GetMongoDBClient(meta, dbName, clientPodName)
			if err != nil {
				fmt.Println("GetMongoDB Client error", err)
				return false
			}
			defer en.Session.Close()

			if err := en.Session.Ping(); err != nil {
				fmt.Println("Ping error", err)
				return false
			}
			person := &KubedbTable{}

			if er := en.Collection("people").FindOne(bson.M{"firstname": "kubernetes"}, person); er == nil {
				return true
			} else {
				fmt.Println("checking error", er)
			}
			return false
		},
		time.Minute*5,
		time.Second*5,
	)
}

func (f *Framework) DocumentExistsInAllInstances(meta metav1.ObjectMeta, dbName string) error {
	mongodb, err := f.GetMongoDB(meta)
	if err != nil {
		return err
	}
	replica := int32(0)
	if mongodb.Spec.Replicas != nil {
		replica = *mongodb.Spec.Replicas
	}
	for i := int32(0); i < replica; i++ {
		clientPodName := fmt.Sprintf("%v-%v", meta.Name, i)
		By("Checking Inserted Document in RS " + clientPodName)
		f.EventuallyDocumentExists(mongodb.ObjectMeta, dbName, clientPodName).Should(BeTrue())
	}

	return nil
}
