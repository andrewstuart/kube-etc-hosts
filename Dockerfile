FROM golang

ENTRYPOINT /kube-etc-hosts

WORKDIR /

ADD kube-etc-hosts /
