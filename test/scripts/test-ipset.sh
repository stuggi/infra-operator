#!/bin/bash
set -x

TESTDIR="/tmp/ipsettest"
rm -rf ${TESTDIR}
mkdir  -p ${TESTDIR}


for INDEX in $(seq -w 1 40); do
  cat <<EOF >${TESTDIR}/edpm-compute-${INDEX}.yaml
apiVersion: network.openstack.org/v1beta1
kind: IPSet
metadata:
  name: edpm-compute-${INDEX}
spec:
  hostname: edpm-compute-${INDEX}
  networks:
  - name: CtlPlane
    subnetName: subnet1
  - name: InternalApi
    subnetName: subnet1
  - name: Storage
    subnetName: subnet1
  - name: Tenant
    subnetName: subnet1
EOF
done


oc apply -f ${TESTDIR}/
