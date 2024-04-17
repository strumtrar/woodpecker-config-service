package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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

	log.Printf("Starting Woodpecker Config Server at: %s\n", envHost)
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

	log.Println("Received the following body for processing:")
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

	if buildPipeline, name, err := getBuildPipeline(req); err != nil {
		log.Printf("Failed to create pipeline: %s", err)
		w.WriteHeader(http.StatusNoContent) // use default config
	} else {
		log.Println("Returning pipeline:\n", string(buildPipeline))
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(map[string]interface{}{"configs": []config{
			{
				Name: name,
				Data: string(buildPipeline),
			},
		}})
		if err != nil {
			log.Printf("Erron on encoding json %v\n", err)
		}
	}
}

func getContent(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Status error: %v", resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("Read body: %v", err)
	}

	return data, nil
}

func getBuildPipeline(req incoming) ([]byte, string, error) {
	var pipelinePath string
	var buildPipelineURL string

	filterRegEx := "YOCTO.*"

	pipelinePath = strings.Replace(req.Build.Branch, "/", "_", -1)
	pipelinePath = strings.Replace(pipelinePath, ".", "_", -1)

	filter := regexp.MustCompile(filterRegEx)
	if filter.MatchString(req.Repo.Name) {
		log.Printf("Got a YOCTO BSP")

		envs := map[string]string{}

		for k,v := range req.Build.AdditionalVariables {
			envs[k] = v
		}

		buildPipelineURL = fmt.Sprintf(
			"%s/raw/branch/master/%s/%s_%s.yaml",
			envPipelines,
			req.Repo.Name,
			pipelinePath,
			envs["Machine"],
		)
	} else {
		buildPipelineURL = fmt.Sprintf(
			"%s/raw/branch/master/%s/%s.yaml",
			envPipelines,
			req.Repo.Name,
			pipelinePath,
		)
	}
	log.Printf("Fetch pipeline from %s", buildPipelineURL)
	b, err := getContent(buildPipelineURL)
	if err != nil {
		buildPipelineURL := fmt.Sprintf(
			"%s/raw/branch/master/%s/default.yaml",
			envPipelines,
			req.Repo.Name,
		)

		log.Printf("Fetch fallback pipeline from %s", buildPipelineURL)
		b, err = getContent(buildPipelineURL)
		if err != nil {
			return nil, "", err
		}
	}

	return b, pipelinePath, nil
}
