REG=andrewstuart
BINARY?=kube-etc-hosts
IMAGE:=$(BINARY)

.PHONY: build push deploy

TAG=$(REG)/$(IMAGE)

$(IMAGE): *.go
	go build -o $(IMAGE)
	upx $(IMAGE)
	
build: $(IMAGE)
	docker build -t $(TAG) .

push: build
	docker push $(TAG)

deploy: push
	kubectl delete po -l app=$(IMAGE)
