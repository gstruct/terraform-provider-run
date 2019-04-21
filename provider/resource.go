package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/terraform/helper/schema"
	toml "github.com/pelletier/go-toml"
	"gopkg.in/yaml.v2"
)

func init() {
	rand.Seed(time.Now().Unix())
}

func resourceRunCommand() *schema.Resource {
	return &schema.Resource{
		Create: resourceRunCommandApply,
		Read:   resourceRunCommandCheck,
		Update: resourceRunCommandApply,
		Delete: resourceRunCommandDestroy,

		Schema: map[string]*schema.Schema{
			"apply": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"check": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"destroy": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"keepers": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: true,
			},

			"shell": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				DefaultFunc: func() (interface{}, error) {
					if runtime.GOOS == "windows" {
						return "cmd /C", nil
					} else {
						return "/bin/sh -c", nil
					}
				},
			},

			"exit_code": {
				Type:     schema.TypeInt,
				Optional: true,
			},

			"exit_code_force_new": {
				Type:     schema.TypeInt,
				Optional: true,
				ForceNew: true,
			},

			"check_input_format": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"check_inputs": {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional: true,
				ForceNew: true,
			},

			"apply_input_format": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"apply_inputs": {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional: true,
				ForceNew: true,
			},

			"destroy_input_format": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"destroy_inputs": {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Optional: true,
			},

			"apply_output_format": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"apply_outputs": {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Computed: true,
			},

			"check_output_format": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"check_outputs": {
				Type: schema.TypeMap,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
				Computed: true,
			},
		},
	}
}

func makeCommand(key string, d *schema.ResourceData) []string {
	argv := strings.Split(d.Get("shell").(string), " ")
	argv = append(argv, d.Get(key).(string))
	return argv
}

func runCommand(argv []string, input []byte) (int, *bytes.Buffer, error) {
	log.Printf("[DEBUG] Running local command: %s", strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return 0, nil, err
	}

	stdoutCh := make(chan []byte)
	done := make(chan bool, 1)
	go func() {
		defer stdin.Close()
		if len(input) > 0 {
			_, err := stdin.Write(input)
			if err != nil {
				log.Printf("[WARN] error writing to stdin: %s", err.Error())
			}
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			out := scanner.Bytes()
			log.Printf("[INFO] stdout: %s", out)
			stdoutCh <- out
		}
		done <- true
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("[INFO] stderr: %s", scanner.Text())
		}
		done <- true
	}()

	if err = cmd.Start(); err != nil {
		return 0, nil, err
	}
	var buf bytes.Buffer
	for i := 0; i < 2; {
		select {
		case line := <-stdoutCh:
			buf.Write(line)
			buf.WriteByte('\n')
		case <-done:
			i++
		}
	}

	var exitCode int
	err = cmd.Wait()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if runtime.GOOS == "windows" && os.Getenv("ERRORLEVEL") != "" {
				errorLevel, err := strconv.ParseInt(os.Getenv("ERRORLEVEL"), 10, 32)
				if err != nil {
					return 0, nil, err
				} else {
					exitCode = int(errorLevel)
				}
			} else if waitStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
				exitCode = waitStatus.ExitStatus()
			} else {
				return 0, nil, err
			}
		} else {
			return 0, nil, err
		}
	}
	return exitCode, &buf, err
}

func makeInput(d *schema.ResourceData, inputType string) ([]byte, error) {
	inputFormat := d.Get(fmt.Sprintf("%s_input_format", inputType)).(string)
	inputs := d.Get(fmt.Sprintf("%s_inputs", inputType)).(map[string]interface{})
	switch strings.ToLower(inputFormat) {
	case "yaml", "yml":
		return yaml.Marshal(inputs)
	case "json":
		return json.Marshal(inputs)
	case "toml":
		return toml.Marshal(inputs)
	case "", "string":
		if v, ok := inputs["string"]; ok {
			return []byte(v.(string)), nil
		}
	default:
		log.Printf("[WARN] Unsupported %s_input_format: %s", inputType, inputFormat)
	}
	return []byte{}, nil
}

func setOutput(d *schema.ResourceData, buf *bytes.Buffer, outputType string) error {
	outputFormat := d.Get(fmt.Sprintf("%s_output_format", outputType)).(string)
	outputs := make(map[string]interface{})
	switch strings.ToLower(outputFormat) {
	case "yaml", "yml":
		if err := yaml.Unmarshal(buf.Bytes(), &outputs); err != nil {
			return err
		}

	case "json":
		if err := json.Unmarshal(buf.Bytes(), &outputs); err != nil {
			return err
		}

	case "hcl":
		obj, err := hcl.Parse(string(buf.Bytes()))
		if err != nil {
			return err
		}
		if err = hcl.DecodeObject(&outputs, obj); err != nil {
			return err
		}

	case "toml":
		tree, err := toml.LoadReader(buf)
		if err != nil {
			return err
		}
		tmap := tree.ToMap()
		for k, v := range tmap {
			outputs[k] = v.(string)
		}
	case "", "string":
		outputs["string"] = buf.String()
	case "null":
		outputs = nil
	default:
		log.Printf("[WARN] Unsupported output_format for resource \"%s\"", d.Id())
		outputs = nil
	}
	var str_outputs map[string]string
	if outputs != nil {
		str_outputs = map_if_to_str(outputs)
	} else {
		str_outputs = nil
	}
	log.Printf("[DEBUG] %s_outputs = \"%+v\"", outputType, str_outputs)
	return d.Set(fmt.Sprintf("%s_outputs", outputType), str_outputs)
}

func resourceRunCommandApply(d *schema.ResourceData, meta interface{}) error {
	input, err := makeInput(d, "apply")
	if err != nil {
		return err
	}
	argv := makeCommand("apply", d)

	_, output, err := runCommand(argv, input)
	if err != nil {
		return err
	}
	if err := setOutput(d, output, "apply"); err != nil {
		return err
	}

	return resourceRunCommandCheck(d, meta)
}

func resourceRunCommandCheck(d *schema.ResourceData, meta interface{}) error {
	input, err := makeInput(d, "check")
	if err != nil {
		return err
	}
	argv := makeCommand("check", d)

	exitCode, output, err := runCommand(argv, input)

	if err != nil && exitCode == 0 {
		return err
	}

	if d.Id() == "" {
		d.SetId(fmt.Sprintf("%d", rand.Int()))
	}

	configExitCodeForceNew := d.Get("exit_code_force_new")
	configExitCode := d.Get("exit_code")

	if configExitCodeForceNew != nil && exitCode != configExitCodeForceNew.(int) {
		d.Set("exit_code_force_new", exitCode)
		d.Set("check_outputs", nil)
		return nil
	} else if configExitCode != nil && exitCode != configExitCode.(int) {
		d.Set("exit_code", exitCode)
		d.Set("check_outputs", nil)
		return nil
	}

	return setOutput(d, output, "check")
}

func resourceRunCommandDestroy(d *schema.ResourceData, meta interface{}) error {
	input, err := makeInput(d, "destroy")
	if err != nil {
		return err
	}
	argv := makeCommand("destroy", d)
	_, _, err = runCommand(argv, input)
	return err
}
