package provider

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/hashicorp/terraform/helper/schema"
	"github.com/spf13/viper"
)

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
				Default:  0,
			},

			"output_format": {
				Type:     schema.TypeString,
				Optional: true,
			},

			"output": {
				Type:     schema.TypeMap,
				Computed: true,
			},
		},
	}
}

func makeCommand(key string, d *schema.ResourceData) *exec.Cmd {
	argv := strings.Split(d.Get("shell").(string), " ")
	argv = append(argv, d.Get(key).(string))
	log.Printf("[DEBUG] Built command-line argv: %+v", argv)
	return exec.Command(argv[0], argv[1:]...)
}

func runCommand(cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	done := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			log.Printf("[INFO] stdout: %s", scanner.Text())
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
		return err
	}
	for i := 0; i < 2; i++ {
		<-done
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func resourceRunCommandApply(d *schema.ResourceData, meta interface{}) error {
	cmd := makeCommand("apply", d)

	if err := runCommand(cmd); err != nil {
		return err
	}

	return resourceRunCommandCheck(d, meta)
}

func resourceRunCommandCheck(d *schema.ResourceData, meta interface{}) error {
	cmd := makeCommand("check", d)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	stdoutCh := make(chan []byte)
	done := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			log.Printf("[INFO] stdout: %s", scanner.Text())
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
		return err
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
	if err := cmd.Wait(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			if runtime.GOOS == "windows" && os.Getenv("ERRORLEVEL") != "" {
				errorLevel, err := strconv.ParseInt(os.Getenv("ERRORLEVEL"), 10, 32)
				if err != nil {
					return err
				} else {
					exitCode = int(errorLevel)
				}
			} else if waitStatus, ok := exitError.Sys().(syscall.WaitStatus); ok {
				exitCode = waitStatus.ExitStatus()
			} else {
				return err
			}
		} else {
			return err
		}
	}

	if exitCode != d.Get("exit_code").(int) {
		d.Set("exit_code", exitCode)
		return nil
	}

	if d.Id() == "" {
		d.SetId(fmt.Sprintf("%d", rand.Int()))
	}

	outputFormat := d.Get("output_format").(string)
        outputMap := make(map[string]string)
	if outputFormat != "" {
		if stringInSlice(outputFormat, viper.SupportedRemoteProviders) {
			output := viper.New()
			output.SetConfigType(outputFormat)
			output.ReadConfig(&buf)
			if err := output.Unmarshal(&outputMap); err != nil {
				return err
			}
			d.Set("output", outputMap)
		} else if outputFormat == "null" {
                        outputMap = nil
		} else {
			log.Printf("[WARN] Unsupported output_format for resource \"%s\"", d.Id())
                        outputMap = nil
		}
	} else {
                outputMap["stdout"] = string(buf.Bytes())
	}
	d.Set("output", outputMap)
	return nil
}

func resourceRunCommandDestroy(d *schema.ResourceData, meta interface{}) error {
	cmd := makeCommand("destroy", d)
	return runCommand(cmd)
}
