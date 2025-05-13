.PHONY: start
start:
	ctlptl apply -f ./cluster.yaml
	kubectl apply -f https://github.com/jetstack/cert-manager/releases/latest/download/cert-manager.yaml
	kubectl -n cert-manager wait --for=condition=available --timeout=180s --all deployments

.PHONY: stop
stop:
	ctlptl delete -f ./cluster.yaml
