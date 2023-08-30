#!/bin/bash

set -euo pipefail

if [ $# != 1 ]; then
	echo "USAGE: $0 <package.tar.gz>"
	exit 1
fi

PROJECT_DIR=$(cd $(dirname $0)/..; pwd)

cd "${PROJECT_DIR}/charts"
mkdir tmp
cp "${PROJECT_DIR}/$1" tmp/
helm repo index --merge index.yaml tmp
mv tmp/index.yaml index.yaml
rm -rf tmp
