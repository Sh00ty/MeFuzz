# syntax=docker/dockerfile:1.2
FROM rust:bullseye AS libafl
LABEL "maintainer"="afl++ team <afl@aflplus.plus>"
LABEL "about"="LibAFL Docker image"

# install sccache to cache subsequent builds of dependencies
RUN cargo install sccache

ENV HOME=/root
ENV SCCACHE_CACHE_SIZE="1G"
ENV SCCACHE_DIR=$HOME/.cache/sccache
ENV RUSTC_WRAPPER="/usr/local/cargo/bin/sccache"
ENV IS_DOCKER="1"
RUN sh -c 'echo set encoding=utf-8 > /root/.vimrc' \
    echo "export PS1='"'[LibAFL \h] \w$(__git_ps1) \$ '"'" >> ~/.bashrc && \
    mkdir ~/.cargo && \
    echo "[build]\nrustc-wrapper = \"${RUSTC_WRAPPER}\"" >> ~/.cargo/config

RUN rustup component add rustfmt clippy

# Install clang 11, common build tools
RUN apt update && apt install -y build-essential gdb git wget clang clang-tools libc++-11-dev libc++abi-11-dev llvm

# Copy a dummy.rs and Cargo.toml first, so that dependencies are cached
WORKDIR /libafl
COPY Cargo.toml README.md ./

COPY fuzzers/libafl_derive/Cargo.toml libafl_derive/Cargo.toml
COPY scripts/dummy.rs libafl_derive/src/lib.rs

COPY libafl/Cargo.toml libafl/build.rs libafl/
COPY libafl/examples libafl/examples
COPY scripts/dummy.rs libafl/src/lib.rs

COPY fuzzers/libafl_frida/Cargo.toml libafl_frida/build.rs libafl_frida/
COPY scripts/dummy.rs libafl_frida/src/lib.rs
COPY fuzzers/libafl_frida/src/gettls.c libafl_frida/src/gettls.c

COPY fuzzers/libafl_qemu/Cargo.toml libafl_qemu/build.rs libafl_qemu/
COPY scripts/dummy.rs libafl_qemu/src/lib.rs

COPY fuzzers/libafl_qemu/libafl_qemu_build/Cargo.toml libafl_qemu/libafl_qemu_build/
COPY scripts/dummy.rs libafl_qemu/libafl_qemu_build/src/lib.rs

COPY fuzzers/libafl_qemu/libafl_qemu_sys/Cargo.toml libafl_qemu/libafl_qemu_sys/build.rs libafl_qemu/libafl_qemu_sys/
COPY scripts/dummy.rs libafl_qemu/libafl_qemu_sys/src/lib.rs

COPY fuzzers/libafl_sugar/Cargo.toml libafl_sugar/
COPY scripts/dummy.rs libafl_sugar/src/lib.rs

COPY fuzzers/libafl_cc/Cargo.toml libafl_cc/Cargo.toml
COPY fuzzers/libafl_cc/build.rs libafl_cc/build.rs
COPY fuzzers/libafl_cc/src libafl_cc/src
COPY scripts/dummy.rs libafl_cc/src/lib.rs

COPY fuzzers/libafl_targets/Cargo.toml libafl_targets/build.rs libafl_targets/
COPY fuzzers/libafl_targets/src libafl_targets/src
COPY scripts/dummy.rs libafl_targets/src/lib.rs

COPY fuzzers/libafl_concolic/test/dump_constraints/Cargo.toml libafl_concolic/test/dump_constraints/
COPY scripts/dummy.rs libafl_concolic/test/dump_constraints/src/lib.rs

COPY fuzzers/libafl_concolic/test/runtime_test/Cargo.toml libafl_concolic/test/runtime_test/
COPY scripts/dummy.rs libafl_concolic/test/runtime_test/src/lib.rs

COPY fuzzers/libafl_concolic/symcc_runtime/Cargo.toml libafl_concolic/symcc_runtime/build.rs libafl_concolic/symcc_runtime/
COPY scripts/dummy.rs libafl_concolic/symcc_runtime/src/lib.rs

COPY fuzzers/libafl_concolic/symcc_libafl/Cargo.toml libafl_concolic/symcc_libafl/
COPY scripts/dummy.rs libafl_concolic/symcc_libafl/src/lib.rs

COPY fuzzers/libafl_nyx/Cargo.toml libafl_nyx/build.rs libafl_nyx/
COPY scripts/dummy.rs libafl_nyx/src/lib.rs

COPY fuzzers/libafl_tinyinst/Cargo.toml libafl_tinyinst/
COPY scripts/dummy.rs libafl_tinyinst/src/lib.rs

COPY utils utils

RUN cargo build && cargo build --release

COPY scripts scripts
COPY docs docs

# Pre-build dependencies for a few common fuzzers

# Dep chain:
# libafl_cc (independent)
# libafl_derive -> libafl
# libafl -> libafl_targets
# libafl_targets -> libafl_frida

# Build once without source
COPY fuzzers/libafl_cc/src libafl_cc/src
RUN touch libafl_cc/src/lib.rs
COPY fuzzers/libafl_derive/src libafl_derive/src
RUN touch libafl_derive/src/lib.rs
COPY libafl/src libafl/src
RUN touch libafl/src/lib.rs
COPY fuzzers/libafl_targets/src libafl_targets/src
RUN touch libafl_targets/src/lib.rs
COPY fuzzers/libafl_frida/src libafl_frida/src
RUN touch libafl_qemu/libafl_qemu_build/src/lib.rs
COPY fuzzers/libafl_qemu/libafl_qemu_build/src libafl_qemu/libafl_qemu_build/src
RUN touch libafl_qemu/libafl_qemu_sys/src/lib.rs
COPY fuzzers/libafl_qemu/libafl_qemu_sys/src libafl_qemu/libafl_qemu_sys/src
RUN touch libafl_qemu/src/lib.rs
COPY fuzzers/libafl_qemu/src libafl_qemu/src
RUN touch libafl_frida/src/lib.rs
COPY fuzzers/libafl_concolic/symcc_libafl libafl_concolic/symcc_libafl
COPY fuzzers/libafl_concolic/symcc_runtime libafl_concolic/symcc_runtime
COPY fuzzers/libafl_concolic/test libafl_concolic/test
COPY fuzzers/libafl_nyx/src libafl_nyx/src
RUN touch libafl_nyx/src/lib.rs
RUN cargo build && cargo build --release

# Copy fuzzers over
COPY fuzzers fuzzers

# RUN ./scripts/test_all_fuzzers.sh --no-fmt

ENTRYPOINT [ "/bin/bash" ]
