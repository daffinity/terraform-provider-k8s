package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/plugin"
	"github.com/hashicorp/terraform/terraform"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: func() terraform.ResourceProvider {
			return &schema.Provider{
				ResourcesMap: map[string]*schema.Resource{
					"k8s_manifest": resourceManifest(),
				},
			}
		},
	})
}

func resourceManifest() *schema.Resource {
	return &schema.Resource{
		Create: resourceManifestCreate,
		Read:   resourceManifestRead,
		Update: resourceManifestUpdate,
		Delete: resourceManifestDelete,

		Schema: map[string]*schema.Schema{
			"content": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
		},
	}
}

func run(cmd *exec.Cmd) error {
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		cmdStr := cmd.Path + " " + strings.Join(cmd.Args, " ")
		if stderr.Len() == 0 {
			return fmt.Errorf("%s: %v", cmdStr, err)
		}
		return fmt.Errorf("%s %v: %s", cmdStr, err, stderr.Bytes())
	}
	return nil
}

func kubectlRun(subcommand, data string) (string, error) {
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd := exec.Command("kubectl", subcommand, "-f", "-", "-o", "json")
	cmd.Stderr = stderr
	cmd.Stdout = stdout
	cmd.Stdin = strings.NewReader(data)
	if err := cmd.Run(); err != nil {
		if stderr.Len() == 0 {
			return "", fmt.Errorf("kubectl %s: %v", subcommand, err)
		}
		return "", fmt.Errorf("kubectl %s %v: %s", subcommand, err, stderr.Bytes())
	}
	log.Printf("%s\n", stdout.String())
	return "", nil
}

func resourceManifestCreate(d *schema.ResourceData, m interface{}) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(d.Get("content").(string))
	if err := run(cmd); err != nil {
		return err
	}

	stdout := &bytes.Buffer{}
	cmd = exec.Command("kubectl", "get", "-f", "-", "-o", "json")
	cmd.Stdin = strings.NewReader(d.Get("content").(string))
	cmd.Stdout = stdout
	if err := run(cmd); err != nil {
		return err
	}

	var data struct {
		Items []struct {
			Metadata struct {
				Selflink string `json:"selflink"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		return fmt.Errorf("decoding response: %v", err)
	}
	if len(data.Items) != 1 {
		return fmt.Errorf("expected to create 1 resource, got %d", len(data.Items))
	}
	selflink := data.Items[0].Metadata.Selflink
	if selflink == "" {
		return fmt.Errorf("could not parse self-link from response %s", stdout.String())
	}
	d.SetId(selflink)
	return nil
}

func resourceManifestUpdate(d *schema.ResourceData, m interface{}) error {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(d.Get("content").(string))
	return run(cmd)
}

func resourceManifestDelete(d *schema.ResourceData, m interface{}) error {
	cmd := exec.Command("kubectl", "delete", "-f", "-")
	cmd.Stdin = strings.NewReader(d.Get("content").(string))
	return run(cmd)
}

func resourceManifestRead(d *schema.ResourceData, m interface{}) error {
	args := []string{"get", "--raw=" + d.Id()}
	stdout := &bytes.Buffer{}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = stdout
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "Error from server (NotFound)") {
		d.SetId("")
		// Ignore this expected error.
		return nil
	}
	if err := run(cmd); err != nil {
		return err
	}
	return nil
}
