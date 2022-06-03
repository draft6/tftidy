package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type empty struct{}
type semaphore chan empty
type moveMapping struct {
	source        string
	target        string
	requiresForce bool
}

var (
	colorReset      = "\033[0m"
	colorRed        = "\033[31m"
	colorGreen      = "\033[32m"
	colorYellow     = "\033[33m"
	colorBlue       = "\033[34m"
	colorPurple     = "\033[35m"
	colorCyan       = "\033[36m"
	colorWhite      = "\033[37m"
	e               = empty{}
	wg              = sync.WaitGroup{}
	sem             semaphore
	lockState       bool
	forceMove       bool
	autoApprove     bool
	sourceNamespace string
	targetNamespace string
	stateLockStr    string
	numThreads      int
)

func init() {
	flag.StringVar(&sourceNamespace, "s", "", "Source resource namespace (prefix)")
	flag.StringVar(&targetNamespace, "t", "", "Target resource namespace (prefix)")
	flag.BoolVar(&lockState, "l", true, "Should we lock the state prior to move?")
	flag.BoolVar(&forceMove, "f", false, "Force move resource, deletes then moves")
	flag.BoolVar(&autoApprove, "y", false, "Auto approve plan")
	flag.IntVar(&numThreads, "n", 1, "Number of threads to use for operations")
}

func callStateMv(source, target string) (string, error) {
	var out bytes.Buffer

	fmt.Printf("%s#>%s Moving <%s> resource to <%s>\n", colorCyan, colorReset, source, target)
	cmd := exec.Command("terraform", "state", "mv", stateLockStr, source, target)
	cmd.Stderr = &out
	err := cmd.Run()

	if err != nil {
		return out.String(), err
	}

	return "", nil
}

func callStateRm(target string) (string, error) {
	var out bytes.Buffer

	fmt.Printf("%s#>%s Deleting <%s> resource\n", colorCyan, colorReset, target)
	cmd := exec.Command("terraform", "state", "rm", stateLockStr, target)
	cmd.Stderr = &out
	err := cmd.Run()

	if err != nil {
		return out.String(), err
	}

	return "", nil
}

func doMove(source, target string) (string, error) {
	return callStateMv(source, target)
}

func doForceMove(source, target string) (string, error) {
	var err error
	var errStr string

	errStr, err = callStateRm(target)
	if err != nil {
		return errStr, err
	}

	return callStateMv(source, target)
}

func strSliceContains(slice []string, str string) bool {
	for _, item := range slice {
		if str == item {
			return true
		}
	}

	return false
}

func printMovePlan(resourceMappings []moveMapping) {
	fmt.Printf("\n\n%s#>%s Following resources will be moved:\n", colorBlue, colorReset)
	for _, mapping := range resourceMappings {
		if mapping.requiresForce {
			fmt.Printf("%s#>%s      <%s> %s=>%s <%s>\n", colorCyan, colorReset, mapping.source, colorPurple, colorReset, mapping.target)
		} else {
			fmt.Printf("%s#>%s      <%s> %s=>%s <%s>\n", colorCyan, colorReset, mapping.source, colorGreen, colorReset, mapping.target)
		}
	}
}

func promptForApprove() {
	fmt.Print("\n\n#> Type 'yes' to continue, 'no' to cancel: ")
	var input string
	fmt.Scanln(&input)

	if input == "yes" {
		return
	}

	if input == "no" {
		fmt.Printf("%s#>%s Cancelled, exiting..\n", colorYellow, colorReset)
		os.Exit(1)
	}

	promptForApprove()
}

func main() {
	flag.Parse()
	sem = make(semaphore, numThreads)
	stateLockStr = fmt.Sprintf("-lock=%v", lockState)
	sourceNamespace = strings.TrimSuffix(sourceNamespace, ".")
	targetNamespace = strings.TrimSuffix(targetNamespace, ".")

	cmd := exec.Command("terraform", "state", "list")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		log.Fatal(err)
	}

	stateResources := strings.Split(out.String(), "\n")
	resourceMappings := []moveMapping{}

	for _, source := range stateResources {
		if strings.HasPrefix(source, sourceNamespace) {
			requiresForce := false
			target := ""
			if targetNamespace != "" {
				target = fmt.Sprintf("%s.%s", targetNamespace, strings.TrimPrefix(source, sourceNamespace+"."))
			} else {
				target = strings.TrimPrefix(source, sourceNamespace+".")
			}

			if strSliceContains(stateResources, target) {
				if forceMove {
					requiresForce = true
				} else {
					fmt.Printf("\n%s#>%s Resource <%s> exists in state, must be force moved (-f flag) \n", colorRed, colorReset, target)
					os.Exit(1)
				}
			}

			resourceMappings = append(resourceMappings, moveMapping{
				source:        source,
				target:        target,
				requiresForce: requiresForce,
			})
		} else if source != "" {
			fmt.Printf("%s#>%s Skipping <%s> resource\n", colorYellow, colorReset, source)
		}
	}

	printMovePlan(resourceMappings)
	if !autoApprove {
		promptForApprove()
	}

	for _, resourceMapping := range resourceMappings {
		wg.Add(1)
		sem <- e
		go func(resourceMapping moveMapping) {
			var err error
			var errStr string
			if resourceMapping.requiresForce {
				errStr, err = doForceMove(resourceMapping.source, resourceMapping.target)
			} else {
				errStr, err = doMove(resourceMapping.source, resourceMapping.target)
			}

			if err != nil {
				fmt.Printf("%s#>%s %s \n", colorRed, colorReset, err)
				fmt.Println(errStr)
			} else {
				fmt.Printf("%s#>%s Succesfully migrated <%s> resource\n", colorGreen, colorReset, resourceMapping.source)
			}
			<-sem
			wg.Done()
		}(resourceMapping)
	}

	wg.Wait()
}
