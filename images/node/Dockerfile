FROM ubuntu:24.04

ARG K3S_VERSION=v1.36.4+k3s1
ARG HELM_VERSION=v4.3.0
ARG K9S_VERSION=v0.51.0
ARG KUSTOMIZE_VERSION=v5.8.1
ARG YQ_VERSION=v4.53.6

ENV DEBIAN_FRONTEND=noninteractive \
    container=docker \
    LANG=C.UTF-8

RUN rm -f /etc/dpkg/dpkg.cfg.d/excludes \
 && echo 'wireshark-common wireshark-common/install-setuid boolean false' | debconf-set-selections \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      systemd systemd-sysv systemd-resolved dbus kmod udev locales tzdata \
      iproute2 net-tools ethtool iputils-ping iputils-arping iputils-tracepath traceroute mtr-tiny \
      netcat-openbsd socat iperf3 telnet nmap arp-scan \
      bind9-dnsutils \
      tcpdump tshark iftop conntrack \
      iptables nftables ufw \
      netplan.io network-manager isc-dhcp-client openssh-server chrony nginx rsyslog \
      snmp snmpd snmp-mibs-downloader lldpd snmptrapd \
      curl wget openssl ca-certificates rsync \
      procps psmisc lsof strace htop sysstat util-linux \
      vim nano less jq tmux tree git bash-completion man-db file unzip sudo cron zstd \
 && apt-get clean && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    curl -fsSL -o /usr/local/bin/k3s "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s"; \
    chmod 0755 /usr/local/bin/k3s; \
    for t in kubectl crictl ctr; do ln -sf k3s /usr/local/bin/$t; done; \
    mkdir -p /usr/share/nsl; \
    curl -fsSL -o /usr/share/nsl/k3s-airgap-images-amd64.tar.zst \
      "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s-airgap-images-amd64.tar.zst"; \
    curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" | tar -xz -C /usr/local/bin --strip-components=1 linux-amd64/helm; \
    curl -fsSL "https://github.com/derailed/k9s/releases/download/${K9S_VERSION}/k9s_Linux_amd64.tar.gz" | tar -xz -C /usr/local/bin k9s; \
    curl -fsSL "https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz" | tar -xz -C /usr/local/bin kustomize; \
    curl -fsSL -o /usr/local/bin/yq "https://github.com/mikefarah/yq/releases/download/${YQ_VERSION}/yq_linux_amd64"; \
    chmod 0755 /usr/local/bin/yq; \
    k3s ctr --help >/dev/null 2>&1

COPY rootfs/ /

RUN set -eux; \
    mkdir -p /var/log/nsl /etc/rancher/k3s; chmod 1777 /var/log/nsl; touch /var/log/nsl/commands.jsonl; chmod 0666 /var/log/nsl/commands.jsonl; \
    useradd -m -s /bin/bash -G sudo nsl; \
    echo '. /etc/profile.d/nsl-history.sh' >> /etc/bash.bashrc; \
    systemctl mask systemd-modules-load.service sys-kernel-debug.mount sys-kernel-config.mount sys-kernel-tracing.mount \
                   systemd-hwdb-update.service systemd-remount-fs.service getty.target console-getty.service \
                   systemd-logind.service ufw.service; \
    systemctl enable systemd-networkd systemd-resolved ssh rsyslog cron chrony; \
    systemctl disable NetworkManager nginx snmpd lldpd snmptrapd; \
    ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf.resolved

STOPSIGNAL SIGRTMIN+3
CMD ["/lib/systemd/systemd"]
