export CONFIG_SERVICE_HOST := "localhost:8003"
export CONFIG_SERVICE_OVERRIDE_FILTER := ".*"
export CONFIG_CONFIGS_REPO := "http://git.igor.lan/str/configs"
export CONFIG_PIPELINES_REPO := "http://git.igor.lan/str/woodpecker-pipelines"

build:
	nix build .#

serve: build
	./result/bin/woodpecker-config-service
