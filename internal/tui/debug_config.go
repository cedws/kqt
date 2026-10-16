package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/z0mbix/kqt/internal/kube"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

type debugProfile struct {
	Image           string         `yaml:"image" toml:"image"`
	Command         []string       `yaml:"command,omitempty" toml:"command,omitempty"`
	PullPolicy      string         `yaml:"image_pull_policy,omitempty" toml:"image_pull_policy,omitempty"`
	SecurityContext map[string]any `yaml:"security_context,omitempty" toml:"security_context,omitempty"`
}
type debugConfig struct {
	DefaultProfile    string                       `yaml:"default_profile,omitempty" toml:"default_profile,omitempty"`
	Profiles          map[string]debugProfile      `yaml:"profiles,omitempty" toml:"profiles,omitempty"`
	ContextProfiles   map[string]string            `yaml:"context_profiles,omitempty" toml:"context_profiles,omitempty"`
	NamespaceProfiles map[string]map[string]string `yaml:"namespace_profiles,omitempty" toml:"namespace_profiles,omitempty"`
	StartTimeout      string                       `yaml:"start_timeout,omitempty" toml:"start_timeout,omitempty"`
}

var debugImagePath = regexp.MustCompile(`^[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*(?:/[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*)*(?::[A-Za-z0-9_][A-Za-z0-9_.-]{0,127})?(?:@sha256:[a-fA-F0-9]{64})?$`)

func compileDebugProfile(p debugProfile) (kube.DebugProfile, error) {
	// Require an explicit registry so an absent/misspelled setting cannot silently
	// pull an unqualified image from Docker Hub. Digests are recommended, not forced.
	registry, image, ok := strings.Cut(p.Image, "/")
	host, err := url.Parse("https://" + registry)
	if !ok || len(p.Image) > 512 || err != nil || host.User != nil || host.Host != registry || host.Hostname() == "" || (!strings.Contains(registry, ".") && !strings.Contains(registry, ":") && registry != "localhost") || !debugImagePath.MatchString(image) {
		return kube.DebugProfile{}, fmt.Errorf("image must be a fully qualified registry/repository reference with an optional tag or sha256 digest")
	}
	if net.ParseIP(host.Hostname()) == nil && len(validation.IsDNS1123Subdomain(strings.ToLower(host.Hostname()))) != 0 {
		return kube.DebugProfile{}, fmt.Errorf("invalid image registry hostname")
	}
	if host.Port() != "" {
		port, err := strconv.Atoi(host.Port())
		if err != nil || port < 1 || port > 65535 {
			return kube.DebugProfile{}, fmt.Errorf("invalid image registry port")
		}
	} else if strings.HasSuffix(registry, ":") {
		return kube.DebugProfile{}, fmt.Errorf("image registry port is empty")
	}
	command := p.Command
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	for _, arg := range command {
		if strings.ContainsRune(arg, 0) {
			return kube.DebugProfile{}, fmt.Errorf("command contains a NUL character")
		}
	}
	if command[0] == "" {
		return kube.DebugProfile{}, fmt.Errorf("command executable must not be empty")
	}
	policy := corev1.PullPolicy(p.PullPolicy)
	if policy == "" {
		policy = corev1.PullIfNotPresent
	}
	if policy != corev1.PullAlways && policy != corev1.PullIfNotPresent && policy != corev1.PullNever {
		return kube.DebugProfile{}, fmt.Errorf("image_pull_policy must be Always, IfNotPresent or Never")
	}
	yes, no := true, false
	security := &corev1.SecurityContext{RunAsNonRoot: &yes, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
	data, err := json.Marshal(p.SecurityContext)
	if err != nil {
		return kube.DebugProfile{}, err
	}
	if p.SecurityContext != nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(security); err != nil {
			return kube.DebugProfile{}, fmt.Errorf("security_context: %w", err)
		}
	}
	if security.RunAsUser != nil && (*security.RunAsUser < 0 || (*security.RunAsUser == 0 && security.RunAsNonRoot != nil && *security.RunAsNonRoot)) {
		return kube.DebugProfile{}, fmt.Errorf("runAsUser must be nonnegative and cannot be 0 with runAsNonRoot")
	}
	if security.RunAsGroup != nil && *security.RunAsGroup < 0 {
		return kube.DebugProfile{}, fmt.Errorf("runAsGroup must be nonnegative")
	}
	return kube.DebugProfile{Image: p.Image, Command: command, PullPolicy: policy, SecurityContext: security}, nil
}

func (c debugConfig) timeout() time.Duration {
	if c.StartTimeout == "" {
		return 90 * time.Second
	}
	d, _ := time.ParseDuration(c.StartTimeout)
	return d
}

func validateDebugConfig(c debugConfig) error {
	if d := c.timeout(); d < time.Second || d > 10*time.Minute {
		return fmt.Errorf("debug.start_timeout must be between 1s and 10m")
	}
	for name, profile := range c.Profiles {
		if !validName(name) || name == "attach" {
			return fmt.Errorf("invalid debug profile name %q", name)
		}
		if _, err := compileDebugProfile(profile); err != nil {
			return fmt.Errorf("debug profile %s: %w", name, err)
		}
	}
	check := func(name string) error {
		if name != "" {
			if _, ok := c.Profiles[name]; !ok {
				return fmt.Errorf("unknown debug profile %q", name)
			}
		}
		return nil
	}
	if err := check(c.DefaultProfile); err != nil {
		return err
	}
	for context, profile := range c.ContextProfiles {
		if strings.TrimSpace(context) == "" || profile == "" {
			return fmt.Errorf("debug.context_profiles requires a context and profile")
		}
		if err := check(profile); err != nil {
			return err
		}
	}
	for context, namespaces := range c.NamespaceProfiles {
		if strings.TrimSpace(context) == "" {
			return fmt.Errorf("debug.namespace_profiles requires a context")
		}
		for namespace, profile := range namespaces {
			if len(validation.IsDNS1123Label(namespace)) != 0 || profile == "" {
				return fmt.Errorf("invalid debug namespace profile %q", namespace)
			}
			if err := check(profile); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c debugConfig) profile(context, namespace string) string {
	if name := c.NamespaceProfiles[context][namespace]; name != "" {
		return name
	}
	if name := c.ContextProfiles[context]; name != "" {
		return name
	}
	return c.DefaultProfile
}
