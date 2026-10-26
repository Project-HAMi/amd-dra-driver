# Copyright 2023 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Copyright (c) Advanced Micro Devices, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the \"License\");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an \"AS IS\" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# HAMi's vGPU hook (libamvgpu.so) from the amd-hami-core submodule, built on the
# same ROCm image the HAMi AMD device plugin uses. The driver installs it on the
# node at startup so GPUSharing can enforce memory slices.
FROM rocm/dev-ubuntu-22.04:7.2.4@sha256:a50a8547101ac9e7c35848ff609a56335214b789b468c9eb05dcd006a4a38c25 as hook
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    cmake build-essential \
    && rm -rf /var/lib/apt/lists/*
COPY amd-hami-core/ /build/amd-hami-core/
RUN cd /build/amd-hami-core && make -f Makefile.hip clean all \
    && test -s build-hip/libamvgpu.so

FROM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc as build

WORKDIR /build
COPY . .

ENV CGO_ENABLED=1
RUN mkdir /artifacts && \
    make PREFIX=/artifacts cmds
# The libc check runs on the host as a CDI hook, so it is built static.
RUN CGO_ENABLED=0 go build -o /artifacts/vgpu-hook-check ./cmd/vgpu-hook-check

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8@sha256:2d4c4551af6831dcd26bbf86108c4385de5183ec8fe6b5868f6f9640fe47f4ed

LABEL io.k8s.display-name="AMD GPU Driver for Dynamic Resource Allocation (DRA)"
LABEL name="AMD GPU Driver for Dynamic Resource Allocation (DRA)"
LABEL vendor="Advanced Micro Devices, Inc."
LABEL maintainer="Nitish Bhat <Nitish.Bhat@amd.com>"
ARG VERSION="latest"
LABEL version=${VERSION}
ARG RELEASE="latest"
LABEL release=${RELEASE}
LABEL summary="AMD DRA GPU driver for Kubernetes"
LABEL description="See summary"

COPY LICENSE LICENSE

RUN microdnf update -y && \
    microdnf install -y libdrm kmod && \
    microdnf clean all

# Copy the vendored AMD SMI library (third_party/amd_smi, sourced from the ROCm
# theRock tarball) for runtime linking, along with the rocm_sysdeps libraries it
# needs: the netlink trio it DT_NEEDEDs, and the DRM pair it dlopens at runtime
# (plus the libdrm* symlinks, the alternative names it tries first).
COPY --from=build /build/third_party/amd_smi/lib/libamd_smi.so* /usr/lib64/
COPY --from=build /build/third_party/amd_smi/lib/librocm_sysdeps_*.so* /usr/lib64/
COPY --from=build /build/third_party/amd_smi/lib/libdrm*.so* /usr/lib64/

COPY --from=build /artifacts/gpu-kubeletplugin /usr/bin/gpu-kubeletplugin
COPY --from=hook /build/amd-hami-core/build-hip/libamvgpu.so /opt/hami/lib/amd/libamvgpu.so
COPY --from=build /artifacts/vgpu-hook-check /opt/hami/lib/amd/vgpu-hook-check
COPY --from=build /artifacts/webhook           /usr/bin/webhook

ENV LD_LIBRARY_PATH=/usr/lib64
