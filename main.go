package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"github.com/BurntSushi/toml"

	"github.com/joho/godotenv"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

type config struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

type branchlut struct {
	Repo string
	Base string
	Pipeline string
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

	// PTXdist image selection, see ptxdistImage()
	envForgeURL	string
	envForgeToken	string
	envImagePrefix	string

	// Repos whose name matches this regex never fall back to the config from
	// the repo itself: without a central pipeline they get a failing one.
	failClosedRe	*regexp.Regexp
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
	envForgeURL = strings.TrimRight(getenvDefault("CONFIG_FORGE_URL", "http://git.leviathan.lan"), "/")
	envForgeToken = os.Getenv("CONFIG_FORGE_TOKEN")
	envImagePrefix = strings.TrimRight(getenvDefault("CONFIG_IMAGE_PREFIX", "git.leviathan.lan/str"), "/")

	if envHost == "" || envConfigs == "" {
		log.Fatal("Please make sure CONFIG_SERVICE_HOST and CONFIG_CONFIGS_REPO is set properly")
	}

	if v := os.Getenv("CONFIG_SERVICE_FAIL_CLOSED_FILTER"); v != "" {
		failClosedRe, err = regexp.Compile(v)
		if err != nil {
			log.Fatalf("Invalid CONFIG_SERVICE_FAIL_CLOSED_FILTER: %v", err)
		}
		log.Printf("Fail closed for repos matching %q", v)
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

	err = json.Unmarshal(body, &req)
	if err != nil {
		http.Error(w, "Failed to parse JSON"+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Repo == nil || req.Build == nil {
		http.Error(w, "missing repo or pipeline in request", http.StatusBadRequest)
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
		// a pipeline that exists but cannot be prepared never falls back
		var perr *prepareError
		if errors.As(err, &perr) {
			serveRejection(w, req, perr.msg)
			return
		}
		if failClosedRe != nil && failClosedRe.MatchString(req.Repo.Name) {
			serveRejection(w, req, "")
			return
		}
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

func getLUT(lutURL string) ([]byte, error) {
	var b []byte
	var buildPipelineURL string
	var lut branchlut

	log.Printf("Fetch LUT from %s", lutURL)
	resp, err := http.Get(lutURL)
	if err != nil {
		return nil, fmt.Errorf("GET error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Status error: %v", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		return nil, err
	}

	log.Printf("Got %q", body)

	meta, err := toml.Decode(string(body[:]), &lut)
	if err != nil {
		log.Printf("Failed to decode toml %v", err)
		return nil, err
	}
	fmt.Println(lut)
	fmt.Println(meta)
	buildPipelineURL = fmt.Sprintf(
		"%s/raw/branch/master/%s/%s",
		envPipelines,
		url.PathEscape(lut.Repo),
		url.PathEscape(lut.Pipeline),
	)
	log.Printf("Fetch pipeline from %s", buildPipelineURL)
	b, err = getContent(buildPipelineURL)
	if err != nil {
		return nil, err
	}

	return b, nil
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
			url.PathEscape(req.Repo.Name),
			url.PathEscape(pipelinePath),
			url.PathEscape(envs["Machine"]),
		)
	} else {
		buildPipelineURL = fmt.Sprintf(
			"%s/raw/branch/master/%s/%s.yaml",
			envPipelines,
			url.PathEscape(req.Repo.Name),
			url.PathEscape(pipelinePath),
		)
	}
	log.Printf("Fetch pipeline from %s", buildPipelineURL)
	b, err := getContent(buildPipelineURL)
	if err != nil {
		var tokens = strings.Split(req.Build.Branch, "/")
		if tokens[0] == "worktree" && len(tokens) > 1 {
			buildPipelineURL = fmt.Sprintf(
				"%s/raw/branch/master/%s/default_%s.yaml",
				envPipelines,
				url.PathEscape(req.Repo.Name),
				url.PathEscape(tokens[1]),
			)
		} else if len(tokens) > 1 && tokens[1] == "topic" {
			buildPipelineURL = fmt.Sprintf(
				"%s/raw/branch/master/lookup/%s.toml",
				envPipelines,
				url.PathEscape(strings.Replace(req.Build.Branch, "/", "_", -1)),
			)
			log.Printf("Try to lookup pipeline from %s", buildPipelineURL)
			b, err = getLUT(buildPipelineURL)
			if err != nil {
				return nil, "", err
			}
			return fillPipeline(req, b, pipelinePath)

		} else {
			buildPipelineURL = fmt.Sprintf(
				"%s/raw/branch/master/%s/default.yaml",
				envPipelines,
				url.PathEscape(req.Repo.Name),
			)
		}

		log.Printf("Fetch fallback pipeline from %s", buildPipelineURL)
		b, err = getContent(buildPipelineURL)
		if err != nil {
			return nil, "", err
		}
	}

	return fillPipeline(req, b, pipelinePath)
}

// serveRejection answers with a pipeline that only fails, so that the config from
// the repo (which may be controlled by an agent) never runs with the secrets of
// the central pipelines. The message is sanitized: repo and branch come from the
// pusher.
func serveRejection(w http.ResponseWriter, req incoming, reason string) {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return r
			case r == '.' || r == '_' || r == '-' || r == '/' || r == ' ' || r == ':' || r == ',' || r == '=':
				return r
			}
			return '?'
		}, s)
	}
	msg := fmt.Sprintf("No central pipeline for repo %s, branch %s. Topic branches need a lookup file lookup/%s.toml in the pipelines repo, which only the maintainer adds.",
		clean(req.Repo.Name), clean(req.Build.Branch),
		clean(strings.Replace(req.Build.Branch, "/", "_", -1)))
	if reason != "" {
		msg = clean(reason)
	}
	data := fmt.Sprintf(`steps:
  - name: no-central-pipeline
    image: docker.io/library/busybox
    commands:
      - echo %q
      - exit 1
`, msg)

	log.Printf("Returning rejection pipeline for %s (%s)", req.Repo.Name, req.Build.Branch)
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(map[string]interface{}{"configs": []config{
		{Name: "rejected", Data: data},
	}})
	if err != nil {
		log.Printf("Error on encoding json %v\n", err)
	}
}

// prepareError: the central pipeline exists but cannot be prepared for this
// commit. The pusher gets a failing pipeline with the reason, never a fallback.
type prepareError struct{ msg string }

func (e *prepareError) Error() string { return e.msg }

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

const ptxdistPlaceholder = "{{PTXDIST_IMAGE}}"

// fillPipeline replaces the placeholders a central pipeline may contain
func fillPipeline(req incoming, b []byte, name string) ([]byte, string, error) {
	if !strings.Contains(string(b), ptxdistPlaceholder) {
		return b, name, nil
	}
	image, err := ptxdistImage(req)
	if err != nil {
		return nil, "", err
	}
	log.Printf("PTXdist image for %s@%s: %s", req.Repo.FullName, req.Build.Commit, image)
	return []byte(strings.ReplaceAll(string(b), ptxdistPlaceholder, image)), name, nil
}

var (
	ptxVersionRe = regexp.MustCompile(`(?m)^PTXCONF_CONFIGFILE_VERSION="([0-9]{4}\.[0-9]{2}\.[0-9]+)"`)
	vendorRe     = regexp.MustCompile(`(?m)^PTXCONF_CROSSCHAIN_VENDOR="OSELAS\.Toolchain-([0-9]{4}\.[0-9]{2})(\.[0-9]+)?"`)
)

// ptxdistImage: the build image for a PTXdist BSP at the pushed commit, from the
// PTXdist version of configs/ptxconfig and the toolchain series (year.month) of
// the platformconfig of the platform in the branch name (worktree/<platform>/...)
func ptxdistImage(req incoming) (string, error) {
	tokens := strings.Split(req.Build.Branch, "/")
	if len(tokens) < 2 || tokens[0] != "worktree" {
		return "", &prepareError{fmt.Sprintf("branch %s: PTXdist pipelines need worktree/<platform>/...", req.Build.Branch)}
	}
	platform := tokens[1]
	if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(platform) {
		return "", &prepareError{"invalid platform in the branch name"}
	}

	ptxconfig, err := repoFile(req, "configs/ptxconfig")
	if err != nil {
		return "", &prepareError{fmt.Sprintf("cannot read configs/ptxconfig: %v", err)}
	}
	m := ptxVersionRe.FindSubmatch(ptxconfig)
	if m == nil {
		return "", &prepareError{"no PTXCONF_CONFIGFILE_VERSION in configs/ptxconfig"}
	}
	ptxdist := string(m[1])

	platformconfig, err := repoFile(req, "configs/platform-"+platform+"/platformconfig")
	if err != nil {
		return "", &prepareError{fmt.Sprintf("cannot read configs/platform-%s/platformconfig: %v", platform, err)}
	}
	m = vendorRe.FindSubmatch(platformconfig)
	if m == nil {
		return "", &prepareError{fmt.Sprintf("no OSELAS.Toolchain-<year>.<month> in PTXCONF_CROSSCHAIN_VENDOR of configs/platform-%s/platformconfig", platform)}
	}
	toolchain := string(m[1])

	repo := fmt.Sprintf("ptxdist-%s-build-env-arm-multi.oselas.toolchain-%s", ptxdist, toolchain)
	image := envImagePrefix + "/" + repo + ":nightly"
	ok, err := imageExists(envImagePrefix+"/"+repo, "nightly")
	if err != nil {
		return "", &prepareError{fmt.Sprintf("cannot check %s: %v", image, err)}
	}
	if !ok {
		return "", &prepareError{fmt.Sprintf("no build image %s for PTXdist %s and toolchain %s, add them to the matrix in build-envs/.forgejo/workflows/docker-ptxdist.yml", image, ptxdist, toolchain)}
	}
	return image, nil
}

// repoFile fetches a file of the pushed commit from the forge
func repoFile(req incoming, path string) ([]byte, error) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(req.Build.Commit) {
		return nil, fmt.Errorf("invalid commit %q", req.Build.Commit)
	}
	u := fmt.Sprintf("%s/%s/raw/commit/%s/%s", envForgeURL, req.Repo.FullName, req.Build.Commit, path)
	r, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if envForgeToken != "" {
		r.Header.Set("Authorization", "token "+envForgeToken)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// imageExists asks the registry (anonymously, the images are public) for the tag
func imageExists(ref, tag string) (bool, error) {
	host, path, ok := strings.Cut(ref, "/")
	if !ok {
		return false, fmt.Errorf("no registry host in %s", ref)
	}
	base := "http://" + host
	scope := url.QueryEscape("repository:" + path + ":pull")
	resp, err := http.Get(base + "/v2/token?scope=" + scope)
	if err != nil {
		return false, err
	}
	var tok struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if err != nil {
		return false, fmt.Errorf("registry token: %v", err)
	}
	r, err := http.NewRequest(http.MethodHead, base+"/v2/"+path+"/manifests/"+tag, nil)
	if err != nil {
		return false, err
	}
	r.Header.Set("Authorization", "Bearer "+tok.Token)
	r.Header.Set("Accept", "application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
	resp, err = http.DefaultClient.Do(r)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}
