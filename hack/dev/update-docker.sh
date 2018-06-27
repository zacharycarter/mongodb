#!/bin/bash
set -xeou pipefail

GOPATH=$(go env GOPATH)
REPO_ROOT=$GOPATH/src/github.com/kubedb/mongodb

# $REPO_ROOT/hack/docker/mongo/3.4/make.sh
# $REPO_ROOT/hack/docker/mongo/3.6/make.sh

$REPO_ROOT/hack/docker/mongo-tools/3.4/make.sh build
$REPO_ROOT/hack/docker/mongo-tools/3.4/make.sh push

$REPO_ROOT/hack/docker/mongo-tools/3.6/make.sh build
$REPO_ROOT/hack/docker/mongo-tools/3.6/make.sh push


# $REPO_ROOT/hack/docker/mg-operator/make.sh build
# $REPO_ROOT/hack/docker/mg-operator/make.sh push

