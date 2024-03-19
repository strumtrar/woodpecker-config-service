package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"github.com/joho/godotenv"
	"go.woodpecker-ci.org/woodpecker/v2/server/model"
)

type config struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

type incoming struct {
	Repo          *model.Repo     `json:"repo"`
	Build         *model.Pipeline `json:"pipeline"`
	Configuration []*config       `json:"configs"`
}

var (
	envConfigs	string
	envPipelines	string
	envFilterRegex	string
	envHost		string
)

func main() {
	log.Println("Woodpecker central config server")

	err := godotenv.Load()
	if err != nil {
		log.Printf("No loadable .env file: %v", err)
	}

	envHost = os.Getenv("CONFIG_SERVICE_HOST")
	envConfigs = os.Getenv("CONFIG_CONFIGS_REPO")
	envPipelines = os.Getenv("CONFIG_PIPELINES_REPO")
	envFilterRegex = os.Getenv("CONFIG_SERVICE_OVERRIDE_FILTER")

	if envHost == "" || envConfigs == "" {
		log.Fatal("Please make sure CONFIG_SERVICE_HOST and CONFIG_CONFIGS_REPO is set properly")
	}

	pipelineHandler := http.HandlerFunc(serveConfig)
	http.HandleFunc("/", pipelineHandler)

	log.Printf("Starting Woodpecer Config Server at: %s\n", envHost)
	err = http.ListenAndServe(envHost, nil)
	if err != nil {
		log.Fatalf("Error on listen: %v", err)
	}
}

func serveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req incoming
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		http.Error(w, "can't read body", http.StatusBadRequest)
		return
	}
	log.Println("Received the following body:")
	log.Println(string(body))

	err = json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "Failed to parse JSON"+err.Error(), http.StatusBadRequest)
		return
	}

	filter := regexp.MustCompile(envFilterRegex)

	if !filter.MatchString(req.Build.Ref) {
		log.Printf("Branch %s does not match filter %s, skipping", req.Build.Ref, envFilterRegex)
		w.WriteHeader(http.StatusNoContent) // use default config
		return
	}

	if buildPipeline, err := getBuildPipeline(req); err != nil {
		log.Printf("Failed to create pipeline: %s", err)
		w.WriteHeader(http.StatusNoContent) // use default config
	} else {
		log.Println("Returning pipeline:\n", string(buildPipeline))
		w.WriteHeader(http.StatusOK)

		if retb, err := w.Write(buildPipeline); err != nil {
			log.Printf("Failed to write the pipeline: %s", err)
		} else {
			log.Printf("%v bytes written", retb)
		}
	}
}

func getBuildPipeline(req incoming) ([]byte, error) {
	buildConfigURL := fmt.Sprintf(
		"'git+%s?ref=%s&rev=%s#woodpecker'",
		envConfigs,
		req.Build.Ref,
		req.Build.Commit,
	)

	var pipelinePath string

	pipelinePath = strings.Replace(req.Build.Ref, "/", "_", -1)
	pipelinePath = strings.Replace(pipelinePath, ".", "_", -1)

	buildPipelineURL := fmt.Sprintf(
		"'git+%s/%s/%s.yaml'",
		envPipelines,
		req.Repo.Name,
		pipelinePath,
	)

	log.Println("Get Configs from:", buildConfigURL)
	log.Println("Get Pipeline from:", buildPipelineURL)

	pwd, _ := os.Getwd()
	pipeline, err := os.ReadFile(filepath.Join(pwd, buildPipelineURL))
	if err != nil {
		return nil, err
	}
	//err = json.NewEncoder(w).Encode(map[string]interface{}{"configs": []config{
	//	{
	//		Name: "central pipe",
	//		Data: overrideConfiguration,
	//	},
	//}})
	//if err != nil {
	//	log.Printf("Error on encoding json %v\n", err)
	//}

	return pipeline, nil
}
