# nifcloud-additional-storage-csi-driver

## Overview

The [NIFCLOUD Additional Storage](https://pfs.nifcloud.com/service/disk.htm) Container Storage Interface (CSI) Driver provides a CSI interface used by Container Orchestrators to manage the lifecycle of NIFCLOUD Additional Storage volumes.

The driver implementation refers to [aws-ebs-csi-driver](https://github.com/kubernetes-sigs/aws-ebs-csi-driver).

**Currently, this is alpha version. So we recommend do not use production environment.**

## Features

The following CSI gRPC calls are implemented:

* **Controller Service**: CreateVolume, DeleteVolume, ControllerPublishVolume, ControllerUnpublishVolume, ControllerGetCapabilities, ValidateVolumeCapabilities
* **NodeService**: NodeStageVolume, NodeUnstageVolume, NodePublishVolume, NodeUnpublishVolume, NodeGetCapabilities, NodeGetInfo
* **Identity Service**: GetPluginInfo, GetPluginCapabilities, Probe


## CreateVolume Parameters

There are several optional parameters that could be passes into CreateVolumeRequest.parameters map:

### csi.storage.k8s.io/fsType
#### description

File system type that will be formatted during volume creation

#### values

* xfs
* ext2
* ext3
* ext4

#### default

ext4

### type
#### description

Storage type (See https://pfs.nifcloud.com/service/disk.htm)

#### values

* standard
* high-speed-a
* high-speed-b
* high-speed (randomly select a or b)
* flash
* standard-flash-a
* standard-flash-b
* standard-flash (randomly select a or b)
* high-speed-flash-a
* high-speed-flash-b
* high-speed-flash (randomly select a or b)

#### default

standard-flash-a


## Installation

1. `git clone https://github.com/aokumasan/nifcloud-additional-storage-csi-driver`
1. `cd nifcloud-additional-storage-csi-driver`
1. Edit `deploy/kubernetes/secret.yaml` and write yout access key and secret key.
1. Edit `deploy/kubernetes/base/controller.yaml` and change region you used. (TODO: read region from configmap)
1. If you use multi zone cluster, delete zone definition and add access secret info to `deploy/kubernetes/base/node.yaml`.
1. `kubectl apply -k deploy/kubernetes/overlays/dev`
