#!/bin/bash

set -eoux pipefail

set +x
DOCKER_USER=${DOCKER_USER:-}
DOCKER_PASS=${DOCKER_PASS:-}

# start docker and log-in to docker-hub
entrypoint.sh
docker login --username=$DOCKER_USER --password=$DOCKER_PASS
set -x
docker run hello-world

# install kubectl
curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl &>/dev/null
chmod +x ./kubectl
mv ./kubectl /bin/kubectl

# install onessl
curl -fsSL -o onessl https://github.com/kubepack/onessl/releases/download/0.6.0/onessl-linux-amd64 &&
  chmod +x onessl &&
  mv onessl /usr/local/bin/

# install pharmer
mkdir -p $GOPATH/src/github.com/pharmer
pushd $GOPATH/src/github.com/pharmer
git clone https://github.com/pharmer/pharmer
cd pharmer
./hack/builddeps.sh
./hack/make.py
popd

function cleanup() {
  set +e

  # Workload Descriptions if the test fails
  cowsay -f tux "Describe Deployment"
  kubectl describe deploy -n kube-system -l app=kubedb
  cowsay -f tux "Describe Replica Set"
  kubectl describe replicasets -n kube-system -l app=kubedb

  cowsay -f tux "Describe Pod"
  kubectl describe pods -n kube-system -l app=kubedb
  cowsay -f tux "Describe Nodes"
  kubectl get nodes
  kubectl describe nodes

  # delete cluster on exit
  pharmer get cluster
  pharmer delete cluster $NAME
  pharmer get cluster
  sleep 120
  pharmer apply $NAME
  pharmer get cluster

  # delete docker image on exit
  curl -LO https://raw.githubusercontent.com/appscodelabs/libbuild/master/docker.py
  chmod +x docker.py
  ./docker.py del_tag kubedbci mg-operator $CUSTOM_OPERATOR_TAG
}
trap cleanup EXIT

# copy mongodb to $GOPATH
mkdir -p $GOPATH/src/github.com/kubedb
cp -r mongodb $GOPATH/src/github.com/kubedb
pushd $GOPATH/src/github.com/kubedb/mongodb

# name of the cluster
NAME=mongodb-$(git rev-parse --short HEAD)

./hack/builddeps.sh
export APPSCODE_ENV=dev
export DOCKER_REGISTRY=kubedbci
./hack/docker/mg-operator/make.sh build
./hack/docker/mg-operator/make.sh push
popd

#create credential file for pharmer
cat >cred.json <<EOF
{
    "token" : "$TOKEN"
}
EOF

# create cluster using pharmer
pharmer create credential --from-file=cred.json --provider=DigitalOcean cred
pharmer create cluster $NAME --provider=digitalocean --zone=nyc1 --nodes=2gb=1 --credential-uid=cred --kubernetes-version=v1.10.0
pharmer apply $NAME
pharmer use cluster $NAME
#wait for cluster to be ready
sleep 300
kubectl get nodes

# create storageclass
cat >sc.yaml <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: standard
parameters:
  zone: nyc1
provisioner: external/pharmer
EOF

# create storage-class
kubectl create -f sc.yaml
sleep 120
kubectl get storageclass

# create config/.env file that have all necessary creds
cp creds/gcs.json /gcs.json
cp creds/.env $GOPATH/src/github.com/kubedb/elasticsearch/hack/config/.env

# run tests
pushd $GOPATH/src/github.com/kubedb/elasticsearch
source ./hack/deploy/setup.sh --docker-registry=kubedbci
./hack/make.py test e2e --v=1 --storageclass=standard --selfhosted-operator=true
