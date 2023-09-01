# nifcloud-additional-storage-csi-driver

## Overview

The [NIFCLOUD Additional Storage](https://pfs.nifcloud.com/service/disk.htm) Container Storage Interface (CSI) Driver provides a CSI interface used by Container Orchestrators to manage the lifecycle of NIFCLOUD Additional Storage volumes.

The driver implementation refers to [aws-ebs-csi-driver](https://github.com/kubernetes-sigs/aws-ebs-csi-driver).

**Currently, this is alpha version. So we recommend do not use production environment.**

## Features

The following CSI gRPC calls are implemented:

- **Controller Service**: CreateVolume, DeleteVolume, ControllerPublishVolume, ControllerUnpublishVolume, ControllerExpandVolume, ControllerGetCapabilities, ValidateVolumeCapabilities
- **NodeService**: NodeStageVolume, NodeUnstageVolume, NodePublishVolume, NodeUnpublishVolume, NodeExpandVolume, NodeGetCapabilities, NodeGetInfo, NodeGetVolumeStats
- **Identity Service**: GetPluginInfo, GetPluginCapabilities, Probe

## CreateVolume Parameters

There are several optional parameters that could be passes into CreateVolumeRequest.parameters map:

### csi.storage.k8s.io/fsType

#### description

File system type that will be formatted during volume creation

#### values

- xfs
- ext2
- ext3
- ext4

#### default

ext4

### type

#### description

Storage type (See https://pfs.nifcloud.com/service/disk.htm)

#### values

- standard
- high-speed-a
- high-speed-b
- high-speed (randomly select a or b)
- flash
- standard-flash-a
- standard-flash-b
- standard-flash (randomly select a or b)
- high-speed-flash-a
- high-speed-flash-b
- high-speed-flash (randomly select a or b)

#### default

standard-flash-a

## Installation

1. Create Secret resource with an NIFCLOUD access key id and secret access key.
   ```yaml
   apiVersion: v1
   kind: Secret
   metadata:
     name: nifcloud-additional-storage-csi-secret
     namespace: kube-system
   stringData:
     access_key_id: ""
     secret_access_key: ""
   ```
2. Add helm repository.
   ```sh
   helm repo add nifcloud-additional-storage-csi-driver https://raw.githubusercontent.com/nifcloud/nifcloud-additional-storage-csi-driver/main/charts
   helm repo update
   ```
3. Install.
   - Please change the parameter `<REGION>` to your environment.
   - See [values.yaml](https://github.com/nifcloud/nifcloud-additional-storage-csi-driver/blob/main/charts/nifcloud-additional-storage-csi-driver/values.yaml) for configurable values.
   ```sh
   helm upgrade --install nifcloud-additional-storage-csi-driver nifcloud-additional-storage-csi-driver/nifcloud-additional-storage-csi-driver \
     --namespace kube-system \
     --set nifcloud.region=<REGION> \
     --set nifcloud.accessKeyId.secretName=nifcloud-additional-storage-csi-secret \
     --set nifcloud.accessKeyId.key=access_key_id \
     --set nifcloud.secretAccessKey.secretName=nifcloud-additional-storage-csi-secret \
     --set nifcloud.secretAccessKey.key=secret_access_key
   ```
