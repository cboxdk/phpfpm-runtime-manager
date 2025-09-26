package resource

import (
	"bufio"
	"os"
	"strings"
)

// ContainerDetector provides container runtime detection capabilities
type ContainerDetector struct {
	// Cached results
	containerInfo *ContainerInfo
	detected      bool
}

// NewContainerDetector creates a new container detector
func NewContainerDetector() *ContainerDetector {
	return &ContainerDetector{}
}

// DetectContainer detects if running in a container and identifies the runtime
func (cd *ContainerDetector) DetectContainer() *ContainerInfo {
	if cd.detected {
		return cd.containerInfo
	}

	cd.containerInfo = &ContainerInfo{
		Runtime: ContainerRuntimeUnknown,
	}

	// Check for various container indicators
	cd.checkDockerEnv()
	cd.checkPodmanEnv()
	cd.checkSystemdContainer()
	cd.checkKubernetes()
	cd.checkCgroups()
	cd.checkContainerRuntime()

	cd.detected = true
	return cd.containerInfo
}

// IsContainerized returns true if running in any container
func (cd *ContainerDetector) IsContainerized() bool {
	info := cd.DetectContainer()
	return info.Runtime != ContainerRuntimeUnknown
}

// checkDockerEnv checks for Docker container environment
func (cd *ContainerDetector) checkDockerEnv() {
	// Check for .dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		cd.containerInfo.Runtime = ContainerRuntimeDocker
		cd.extractDockerInfo()
		return
	}

	// Check for Docker in cgroup
	if cd.checkCgroupForDocker() {
		cd.containerInfo.Runtime = ContainerRuntimeDocker
		cd.extractDockerInfo()
	}
}

// checkPodmanEnv checks for Podman container environment
func (cd *ContainerDetector) checkPodmanEnv() {
	// Check for Podman container environment file
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		cd.containerInfo.Runtime = ContainerRuntimePodman
		cd.extractPodmanInfo()
		return
	}

	// Check for Podman in cgroup
	if cd.checkCgroupForPodman() {
		cd.containerInfo.Runtime = ContainerRuntimePodman
		cd.extractPodmanInfo()
	}
}

// checkSystemdContainer checks for systemd container
func (cd *ContainerDetector) checkSystemdContainer() {
	// Check systemd container environment
	if container := os.Getenv("container"); container != "" {
		if cd.containerInfo.Runtime == ContainerRuntimeUnknown {
			cd.containerInfo.Runtime = ContainerRuntimeSystemd
		}
	}

	// Check for systemd-nspawn
	if cd.checkCgroupForSystemd() {
		if cd.containerInfo.Runtime == ContainerRuntimeUnknown {
			cd.containerInfo.Runtime = ContainerRuntimeSystemd
		}
	}
}

// checkKubernetes checks for Kubernetes environment
func (cd *ContainerDetector) checkKubernetes() {
	// Check for Kubernetes service account
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount"); err == nil {
		cd.containerInfo.Orchestrator = "kubernetes"
		cd.extractKubernetesInfo()
	}

	// Check for Kubernetes environment variables
	if cd.checkKubernetesEnvVars() {
		cd.containerInfo.Orchestrator = "kubernetes"
		cd.extractKubernetesInfo()
	}
}

// checkCgroups checks cgroup information for container detection
func (cd *ContainerDetector) checkCgroups() {
	// Read /proc/self/cgroup for container information
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		cd.parseCgroupLine(line)
	}
}

// checkContainerRuntime checks for specific container runtime indicators
func (cd *ContainerDetector) checkContainerRuntime() {
	// Check for containerd
	if cd.checkCgroupForContainerd() {
		if cd.containerInfo.Runtime == ContainerRuntimeUnknown {
			cd.containerInfo.Runtime = ContainerRuntimeContainerd
		}
	}

	// Check for CRI-O
	if cd.checkCgroupForCRIO() {
		if cd.containerInfo.Runtime == ContainerRuntimeUnknown {
			cd.containerInfo.Runtime = ContainerRuntimeCRIO
		}
	}

	// Check for LXC
	if cd.checkCgroupForLXC() {
		if cd.containerInfo.Runtime == ContainerRuntimeUnknown {
			cd.containerInfo.Runtime = ContainerRuntimeLXC
		}
	}
}

// extractDockerInfo extracts Docker-specific information
func (cd *ContainerDetector) extractDockerInfo() {
	// Try to get container ID from hostname
	if hostname, err := os.Hostname(); err == nil {
		// Docker containers often use container ID as hostname
		if len(hostname) == 12 || len(hostname) == 64 {
			cd.containerInfo.ID = hostname
		}
	}

	// Try to extract from cgroup
	cd.extractContainerIDFromCgroup("docker")
}

// extractPodmanInfo extracts Podman-specific information
func (cd *ContainerDetector) extractPodmanInfo() {
	// Read container environment file if available
	if data, err := os.ReadFile("/run/.containerenv"); err == nil {
		cd.parsePodmanEnv(string(data))
	}

	// Try to extract from cgroup
	cd.extractContainerIDFromCgroup("podman")
}

// extractKubernetesInfo extracts Kubernetes-specific information
func (cd *ContainerDetector) extractKubernetesInfo() {
	// Get pod name from environment
	if podName := os.Getenv("POD_NAME"); podName != "" {
		cd.containerInfo.PodName = podName
	} else if hostname, err := os.Hostname(); err == nil {
		cd.containerInfo.PodName = hostname
	}

	// Get namespace from environment or service account
	if namespace := os.Getenv("POD_NAMESPACE"); namespace != "" {
		cd.containerInfo.Namespace = namespace
	} else {
		cd.extractNamespaceFromServiceAccount()
	}

	// Get container name from environment
	if containerName := os.Getenv("CONTAINER_NAME"); containerName != "" {
		cd.containerInfo.Name = containerName
	}
}

// Helper methods for cgroup parsing

func (cd *ContainerDetector) checkCgroupForDocker() bool {
	return cd.checkCgroupPattern("docker")
}

func (cd *ContainerDetector) checkCgroupForPodman() bool {
	return cd.checkCgroupPattern("podman")
}

func (cd *ContainerDetector) checkCgroupForSystemd() bool {
	return cd.checkCgroupPattern("machine.slice")
}

func (cd *ContainerDetector) checkCgroupForContainerd() bool {
	return cd.checkCgroupPattern("containerd")
}

func (cd *ContainerDetector) checkCgroupForCRIO() bool {
	return cd.checkCgroupPattern("crio")
}

func (cd *ContainerDetector) checkCgroupForLXC() bool {
	return cd.checkCgroupPattern("lxc")
}

func (cd *ContainerDetector) checkCgroupPattern(pattern string) bool {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

func (cd *ContainerDetector) parseCgroupLine(line string) {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return
	}

	path := parts[2]

	// Extract container ID from various patterns
	if strings.Contains(path, "/docker/") {
		cd.extractContainerIDFromPath(path, "/docker/")
	} else if strings.Contains(path, "/podman/") {
		cd.extractContainerIDFromPath(path, "/podman/")
	} else if strings.Contains(path, "/system.slice/") {
		cd.extractSystemdContainerInfo(path)
	}
}

func (cd *ContainerDetector) extractContainerIDFromPath(path, pattern string) {
	index := strings.Index(path, pattern)
	if index == -1 {
		return
	}

	remaining := path[index+len(pattern):]
	parts := strings.Split(remaining, "/")
	if len(parts) > 0 && len(parts[0]) >= 12 {
		cd.containerInfo.ID = parts[0]
	}
}

func (cd *ContainerDetector) extractContainerIDFromCgroup(runtime string) {
	file, err := os.Open("/proc/self/cgroup")
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, runtime) {
			cd.extractContainerIDFromPath(line, runtime)
			break
		}
	}
}

func (cd *ContainerDetector) extractSystemdContainerInfo(path string) {
	// Extract systemd container information
	if strings.Contains(path, "machine.slice") {
		parts := strings.Split(path, "/")
		for _, part := range parts {
			if strings.HasPrefix(part, "machine-") && strings.HasSuffix(part, ".scope") {
				// Extract machine name
				machineName := strings.TrimPrefix(part, "machine-")
				machineName = strings.TrimSuffix(machineName, ".scope")
				cd.containerInfo.Name = machineName
				break
			}
		}
	}
}

func (cd *ContainerDetector) parsePodmanEnv(content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")

		switch key {
		case "name":
			cd.containerInfo.Name = value
		case "id":
			cd.containerInfo.ID = value
		case "image":
			cd.containerInfo.Image = value
		}
	}
}

func (cd *ContainerDetector) checkKubernetesEnvVars() bool {
	k8sEnvVars := []string{
		"KUBERNETES_SERVICE_HOST",
		"KUBERNETES_SERVICE_PORT",
		"POD_NAME",
		"POD_NAMESPACE",
	}

	for _, envVar := range k8sEnvVars {
		if os.Getenv(envVar) != "" {
			return true
		}
	}
	return false
}

func (cd *ContainerDetector) extractNamespaceFromServiceAccount() {
	namespacePath := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	if data, err := os.ReadFile(namespacePath); err == nil {
		cd.containerInfo.Namespace = strings.TrimSpace(string(data))
	}
}

// GetContainerID attempts to get the container ID from various sources
func (cd *ContainerDetector) GetContainerID() string {
	info := cd.DetectContainer()
	if info.ID != "" {
		return info.ID
	}

	// Try to get from hostname as fallback
	if hostname, err := os.Hostname(); err == nil {
		if len(hostname) == 12 || len(hostname) == 64 {
			return hostname
		}
	}

	return ""
}

// GetContainerName attempts to get the container name
func (cd *ContainerDetector) GetContainerName() string {
	info := cd.DetectContainer()
	return info.Name
}

// GetOrchestrator returns the detected orchestrator (kubernetes, swarm, etc.)
func (cd *ContainerDetector) GetOrchestrator() string {
	info := cd.DetectContainer()
	return info.Orchestrator
}

// Reset clears cached detection results
func (cd *ContainerDetector) Reset() {
	cd.detected = false
	cd.containerInfo = nil
}
