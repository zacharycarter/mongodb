#!/bin/bash
set -exou pipefail

GOPATH=$(go env GOPATH)
DOCKER_REGISTRY=${DOCKER_REGISTRY:-kubedb}

REPO_ROOT="$GOPATH/src/github.com/kubedb/mongodb"

# run in minikube
$REPO_ROOT/hack/dev/toolbox.sh --uninstall --purge
$REPO_ROOT/hack/make.py
$REPO_ROOT/hack/dev/setup.sh
mongodb run --docker-registry=${DOCKER_REGISTRY} \
    --secure-port=8443 \
    --kubeconfig="$HOME/.kube/config" \
    --authorization-kubeconfig="$HOME/.kube/config" \
    --authentication-kubeconfig="$HOME/.kube/config"

