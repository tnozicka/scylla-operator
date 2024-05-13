#!/usr/bin/env bash
#
# Copyright (C) 2023 ScyllaDB
#

set -euExo pipefail
shopt -s inherit_errexit

if [ -z "${ARTIFACTS+x}" ]; then
  echo "ARTIFACTS can't be empty" > /dev/stderr
  exit 2
fi

if [ -z "${KUBECONFIG_DIR+x}" ]; then
  kubeconfigs=("${KUBECONFIG}")
else
  kubeconfigs=()
  for f in $( find "$( realpath "${KUBECONFIG_DIR}" )" -maxdepth 1 -type f -name '*.kubeconfig' ); do
    kubeconfigs+=("${f}")
  done
fi

source "$( dirname "${BASH_SOURCE[0]}" )/../lib/kube.sh"
source "$( dirname "${BASH_SOURCE[0]}" )/lib/e2e.sh"
parent_dir="$( dirname "${BASH_SOURCE[0]}" )"

trap "gather-multi-cluster-artifacts-on-exit ${kubeconfigs[@]}" EXIT

SO_NODECONFIG_PATH="${SO_NODECONFIG_PATH=./hack/.ci/manifests/cluster/nodeconfig.yaml}"
export SO_NODECONFIG_PATH
SO_CSI_DRIVER_PATH="${parent_dir}/manifests/namespaces/local-csi-driver/"
export SO_CSI_DRIVER_PATH

# Backwards compatibility. Remove when release repo stops using SO_DISABLE_NODECONFIG.
if [[ "${SO_DISABLE_NODECONFIG:-false}" == "true" ]]; then
  SO_NODECONFIG_PATH=""
  SO_CSI_DRIVER_PATH=""
else
  # Make sure there is no default storage class before we create our own.
  for i in "${!kubeconfigs[@]}"; do
    KUBECONFIG="${kubeconfigs[$i]}" unset-default-storageclass
  done
fi

SCYLLA_OPERATOR_FEATURE_GATES="${SCYLLA_OPERATOR_FEATURE_GATES:-AllAlpha=true,AllBeta=true}"
export SCYLLA_OPERATOR_FEATURE_GATES

for i in "${!kubeconfigs[@]}"; do
  KUBECONFIG="${kubeconfigs[$i]}" timeout --foreground -v 10m "${parent_dir}/../ci-deploy.sh" "${SO_IMAGE}" &
  ci_deploy_bg_pids["${i}"]=$!
done

for pid in "${ci_deploy_bg_pids[@]}"; do
  wait "${pid}"
done

KUBECONFIG="${kubeconfigs[0]}" apply-e2e-workarounds
KUBECONFIG="${kubeconfigs[0]}" run-e2e
