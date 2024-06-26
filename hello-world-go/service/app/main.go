package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/negroni"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var tmpl *template.Template

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Request: %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func logResponse(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			log.Printf("Response: %s %s", r.Method, r.URL.Path)
		}()
		next.ServeHTTP(w, r)
	})
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Healthy!")
	w.WriteHeader(http.StatusOK)
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		log.Println("Error getting hostname:", err)
		return "Unknown"
	}
	return hostname
}

func getRequestHeaders(r *http.Request) http.Header {
	return r.Header
}

func getKubernetesPort() string {
	return os.Getenv("KUBERNETES_SERVICE_PORT")
}

func getKubernetesHost() string {
	return os.Getenv("KUBERNETES_SERVICE_HOST")
}

func getAppIP() string {
	return os.Getenv("HELLO_WORLD_PORT")
}

func getSvcIP() string {
	return os.Getenv("SVC_IP")
}

// a lot of these structs are for later work
func helloHandler(w http.ResponseWriter, r *http.Request) {
	// Check if tmpl is nil
	if tmpl == nil {
		http.Error(w, "Internal server error: Template is nil", http.StatusInternalServerError)
		return
	}

	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	podUID := os.Getenv("POD_UID")
	podCreationTimestamp := os.Getenv("POD_CREATION_TIMESTAMP")
	podLabels := parseMapEnv(os.Getenv("POD_LABELS"))
	podAnnotations := parseMapEnv(os.Getenv("POD_ANNOTATIONS"))

	// Get the user running the app
	currentUser, err := user.Current()
	if err != nil {
		log.Printf("Error getting current user: %v", err)
	}
	userName := currentUser.Username

	// Prepare the message based on the user
	var userMessage string
	if userName == "root" {
		userMessage = "Dude...wtf are you doing running as root?"
	} else {
		userMessage = fmt.Sprintf("Good thing the user '%s' is running this container.", userName)
	}

	data := struct {
		Hostname             string
		Headers              http.Header // Define the Headers field
		AppIP                string
		K8sHost              string
		K8sPort              string
		PodName              string
		PodNamespace         string
		PodUID               string
		PodCreationTimestamp string
		PodLabels            map[string]string
		PodAnnotations       map[string]string
		UserMessage          string
		SvcIP                string
	}{
		Hostname:             getHostname(),
		Headers:              getRequestHeaders(r), 
		AppIP:                getAppIP(),
		K8sHost:              getKubernetesHost(),
		K8sPort:              getKubernetesPort(),
		PodName:              podName,
		PodNamespace:         podNamespace,
		PodUID:               podUID,
		PodCreationTimestamp: podCreationTimestamp,
		PodLabels:            podLabels,
		PodAnnotations:       podAnnotations,
		UserMessage:          userMessage,
		SvcIP:                getSvcIP(),
	}

	if err := tmpl.Execute(w, data); err != nil {
		log.Println("Error executing template:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func parseMapEnv(envStr string) map[string]string {
	m := make(map[string]string)
	for _, kv := range strings.Split(envStr, ",") {
		pair := strings.Split(kv, "=")
		if len(pair) == 2 {
			m[pair[0]] = pair[1]
		}
	}
	return m
}

func containerInfoHandler(w http.ResponseWriter, r *http.Request) {
	if _, hostExists := os.LookupEnv("KUBERNETES_SERVICE_HOST"); !hostExists {
		// Kubernetes environment variables are not present, so this container is not running in Kubernetes
		fmt.Fprintln(w, "This container isn't running in Kubernetes.")
		return
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		errorMsg := fmt.Sprintf("Error initializing Kubernetes client config: %s", err)
		log.Println(errorMsg)
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		errorMsg := fmt.Sprintf("Error creating Kubernetes client: %s", err)
		log.Println(errorMsg)
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")

	pod, err := clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		errorMsg := fmt.Sprintf("Error retrieving pod information: %s", err)
		log.Println(errorMsg)
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	var containerInfo strings.Builder
	for _, container := range pod.Spec.Containers {
		containerInfo.WriteString("Container Name: " + container.Name + "\n")
		containerInfo.WriteString("Image: " + container.Image + "\n")
		containerInfo.WriteString("Ports:\n")
		for _, port := range container.Ports {
			containerInfo.WriteString(fmt.Sprintf("- %d:%d\n", port.ContainerPort, port.HostPort))
		}
		containerInfo.WriteString("\n")

		// Additional Information
		containerInfo.WriteString("Namespace: " + pod.Namespace + "\n")

		containerInfo.WriteString("Resources:\n")
		containerInfo.WriteString(fmt.Sprintf("- CPU Limit: %s\n", container.Resources.Limits.Cpu().String()))
		containerInfo.WriteString(fmt.Sprintf("- Memory Limit: %s\n", container.Resources.Limits.Memory().String()))
	
		containerInfo.WriteString(fmt.Sprintf("- CPU Request: %s\n", container.Resources.Requests.Cpu().String()))
		containerInfo.WriteString(fmt.Sprintf("- Memory Request: %s\n", container.Resources.Requests.Memory().String()))

		containerInfo.WriteString("Volume Mounts:\n")
		for _, volumeMount := range container.VolumeMounts {
			containerInfo.WriteString(fmt.Sprintf("- Name: %s, Mount Path: %s\n", volumeMount.Name, volumeMount.MountPath))
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(containerInfo.String()))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("Ready to receive requests on port %s\n", port)

	// Initialize the template by parsing the index.html file
	var err error
	tmpl, err = template.ParseFiles("/app/templates/index.html")
	if err != nil {
		log.Fatal("Error parsing template:", err)
	}

	// Serve static files
	fs := http.FileServer(http.Dir("./static/img"))
	http.Handle("/static/img/", http.StripPrefix("/static/img/", fs))

	// Log each request and its response
	http.HandleFunc("/favicon.ico", faviconHandler)
	http.HandleFunc("/healthz", healthzHandler)
	http.HandleFunc("/", helloHandler)
	http.HandleFunc("/container-info", containerInfoHandler)

	// Use Negroni for logging middleware
	n := negroni.Classic()
	n.UseHandler(http.DefaultServeMux)

	// Create an HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", port),
		Handler: n,
	}

	// Channel to listen for termination signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Goroutine to start the server
	go func() {
		log.Println("Starting server...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on %s: %v\n", port, err)
		}
	}()

	// Block until a termination signal is received
	<-stop

	// Gracefully shut down the server
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped gracefully")
}