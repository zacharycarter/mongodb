#!/bin/bash
set -xeou pipefail

GOPATH=$(go env GOPATH)
REPO_ROOT=$GOPATH/src/github.com/kubedb/mongodb

source "$REPO_ROOT/hack/libbuild/common/lib.sh"
source "$REPO_ROOT/hack/libbuild/common/kubedb_image.sh"

DOCKER_REGISTRY=${DOCKER_REGISTRY:-kubedb}
IMG=mongo-init

build() {
    pushd "$REPO_ROOT/hack/docker/mongo-init"

    local cmd="docker build -t $DOCKER_REGISTRY/$IMG ."
    echo $cmd; $cmd

    popd
}

binary_repo $@
