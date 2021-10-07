package main

import (
	"cloud.google.com/go/compute/metadata"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/serviceusage/v1"
	"github.com/PuerkitoBio/rehttp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"github.com/prometheus/common/version"
	"golang.org/x/oauth2/google"
	"time"

	"google.golang.org/api/option"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/tidwall/gjson"
)

var (
	limitDesc          = prometheus.NewDesc("gcp_quota_limit", "quota limits for GCP components", []string{"project", "region", "metric"}, nil)
	usageDesc          = prometheus.NewDesc("gcp_quota_usage", "quota usage for GCP components", []string{"project", "region", "metric"}, nil)
	projectQuotaUpDesc = prometheus.NewDesc("gcp_quota_project_up", "Was the last scrape of the Google Project API successful.", []string{"project"}, nil)
	regionsQuotaUpDesc = prometheus.NewDesc("gcp_quota_regions_up", "Was the last scrape of the Google Regions API successful.", []string{"project"}, nil)

	gcpProjectID = kingpin.Flag(
		"gcp.project_id", "ID of the Google Project to be monitored. ($GOOGLE_PROJECT_ID)",
	).Envar("GOOGLE_PROJECT_ID").String()

	gcpParenfFolderID = kingpin.Flag(
		"gcp.folder_id", "ID of the Google Folder where Projects to be monitored. ($GOOGLE_FOLDER_ID)",
	).Envar("GOOGLE_FOLDER_ID").String()

	gcpMaxRetries = kingpin.Flag(
		"gcp.max-retries", "Max number of retries that should be attempted on 503 errors from gcp. ($GCP_EXPORTER_MAX_RETRIES)",
	).Envar("GCP_EXPORTER_MAX_RETRIES").Default("0").Int()

	gcpHttpTimeout = kingpin.Flag(
		"gcp.http-timeout", "How long should gcp_exporter wait for a result from the Google API ($GCP_EXPORTER_HTTP_TIMEOUT)",
	).Envar("GCP_EXPORTER_HTTP_TIMEOUT").Default("10s").Duration()

	gcpMaxBackoffDuration = kingpin.Flag(
		"gcp.max-backoff", "Max time between each request in an exp backoff scenario ($GCP_EXPORTER_MAX_BACKOFF_DURATION)",
	).Envar("GCP_EXPORTER_MAX_BACKOFF_DURATION").Default("5s").Duration()

	gcpBackoffJitterBase = kingpin.Flag(
		"gcp.backoff-jitter", "The amount of jitter to introduce in a exp backoff scenario ($GCP_EXPORTER_BACKODFF_JITTER_BASE)",
	).Envar("GCP_EXPORTER_BACKOFF_JITTER_BASE").Default("1s").Duration()

	gcpRetryStatuses = kingpin.Flag(
		"gcp.retry-statuses", "The HTTP statuses that should trigger a retry ($GCP_EXPORTER_RETRY_STATUSES)",
	).Envar("GCP_EXPORTER_RETRY_STATUSES").Default("503").Ints()
)

// Exporter collects quota stats from the Google Compute API and exports them using the Prometheus metrics package.
type Exporter struct {
	computeService *compute.Service
	resourceManagerService *cloudresourcemanager.Service
	serviceusageService *serviceusage.Service
	folder string
	project string
	mutex   sync.RWMutex
	ProjectsMap map[string]Projects
}

type Projects struct {
	project *compute.Project
	regionList *compute.RegionList
}

func (e *Exporter) getPeojectsByFolder() ([]string){
	projectsList := [] string{}
	ctx := context.Background()
	req := e.resourceManagerService.Projects.List()
	req = req.Filter("parent.id=" + e.folder)
	if err := req.Pages(ctx, func(page *cloudresourcemanager.ListProjectsResponse) error {
		for _, project := range page.Projects {
			//serviceReq := e.serviceusageService.Services.BatchGet("projects/"+project.ProjectId)
			//batchgetSer, _ := serviceReq.Context(ctx).Do()
			//fmt.Printf("SERVICES:::%#v\n", batchgetSer.Services)
			//fmt.Printf("\n\n\nprojects/%#v\n\n",project.ProjectId)
			projNum := fmt.Sprintf("projects/%d", project.ProjectNumber)
			serviceReq := e.serviceusageService.Services.List(projNum)
			serviceReq = serviceReq.Filter("state:ENABLED")
			serviceslist, err := serviceReq.Context(ctx).Do()
			computeEnabled := false
			if err == nil && serviceslist != nil {
				for _, service := range serviceslist.Services {
					//serviceName := strings.Split(service.Name, "/services/")[1]
					if strings.Contains(service.Name, "compute.googleapis.com") {
						computeEnabled = true
						break
					}
				}
			}
			if computeEnabled {
				projectsList = append(projectsList, project.ProjectId)
			}
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	//req1 = serviceusageService.Services.List("projects/"+)
	return projectsList
}

// scrape connects to the Google API to retreive quota statistics and record them as metrics.

func (e *Exporter) scrape() (map[string]Projects) {//(prj *compute.Project, rgl *compute.RegionList) {
	var resultMap map[string]Projects
	var projectsList []string

	resultMap = map[string]Projects{}
	if e.folder != "" {
		projectsList = e.getPeojectsByFolder()
		//fmt.Printf("%#v\n",projectsList)
		//projectsList = []string{e.project}
	} else {
		projectsList = []string{e.project}
	}
	for _, projectId := range projectsList {
		project, err := e.computeService.Projects.Get(projectId).Do()
		if err != nil {
			log.Fatalf("Failure when querying project quotas: %v", err)
			project = nil
		}

		regionList, err := e.computeService.Regions.List(projectId).Do()
		if err != nil {
			log.Fatalf("Failure when querying region quotas: %v", err)
			regionList = nil
		}
		//fmt.Print(e.project, project, regionList )
		resultMap[projectId] = Projects{
			project:    project,
			regionList: regionList,
		}
	}
	return resultMap


	//log.Printf("Start scraping Google quotas for projects in folder %s", e.folder)
	/*for {
		resultMap = map[string]Projects{}
		if e.folder != "" {
			projectsList = e.getPeojectsByFolder()
			//fmt.Printf("%#v\n",projectsList)
			//projectsList = []string{e.project}
		} else {
			projectsList = []string{e.project}
		}
		for _, projectId := range projectsList {
			project, err := e.computeService.Projects.Get(projectId).Do()
			if err != nil {
				log.Fatalf("Failure when querying project quotas: %v", err)
				project = nil
			}

			regionList, err := e.computeService.Regions.List(projectId).Do()
			if err != nil {
				log.Fatalf("Failure when querying region quotas: %v", err)
				regionList = nil
			}
			//fmt.Print(e.project, project, regionList )
			resultMap[projectId] = Projects{
				project:    project,
				regionList: regionList,
			}
		}
		//fmt.Printf("SCRAAAAAAAAAAP",resultMap)
		e.ProjectsMap = resultMap
		time.Sleep(30 * time.Second)
	}*/
}

func (e *Exporter) scrapeRoutine() {
	log.Printf("Start scraping Google quotas for projects in folder %s", e.folder)
	for {
		e.ProjectsMap = e.scrape()
		time.Sleep(30 * time.Second)
	}

}

// Describe is implemented with DescribeByCollect. That's possible because the
// Collect method will always return the same metrics with the same descriptors.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(e, ch)
}


// Collect will run each time the exporter is polled and will in turn call the
// Google API for the required statistics.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.mutex.Lock() // To protect metrics from concurrent collects.
	defer e.mutex.Unlock()
	//fmt.Println(e.ProjectsMap)
	for pName, pValue := range e.ProjectsMap {
		if pValue.project != nil {
			for _, quota := range pValue.project.Quotas {
				ch <- prometheus.MustNewConstMetric(limitDesc, prometheus.GaugeValue, quota.Limit, pName, "", quota.Metric)
				ch <- prometheus.MustNewConstMetric(usageDesc, prometheus.GaugeValue, quota.Usage, pName, "", quota.Metric)
			}
			ch <- prometheus.MustNewConstMetric(projectQuotaUpDesc, prometheus.GaugeValue, 1, pName)
		} else {
			ch <- prometheus.MustNewConstMetric(projectQuotaUpDesc, prometheus.GaugeValue, 0, pName)

		}

		if pValue.regionList != nil {
			for _, region := range pValue.regionList.Items {
				regionName := region.Name
				for _, quota := range region.Quotas {
					ch <- prometheus.MustNewConstMetric(limitDesc, prometheus.GaugeValue, quota.Limit, pName, regionName, quota.Metric)
					ch <- prometheus.MustNewConstMetric(usageDesc, prometheus.GaugeValue, quota.Usage, pName, regionName, quota.Metric)
				}
			}
			ch <- prometheus.MustNewConstMetric(regionsQuotaUpDesc, prometheus.GaugeValue, 1, pName)
		} else {
			ch <- prometheus.MustNewConstMetric(regionsQuotaUpDesc, prometheus.GaugeValue, 0, pName)
		}
	}
}

// NewExporter returns an initialised Exporter.
func NewExporter(project, folder string) (*Exporter, error) {
	// Create context and generate compute.Service
	ctx := context.Background()

	googleClient, err := google.DefaultClient(ctx, compute.ComputeReadonlyScope, cloudresourcemanager.CloudPlatformReadOnlyScope)
	if err != nil {
		return nil, fmt.Errorf("Error creating Google client: %v", err)
	}

	googleClient.Timeout = *gcpHttpTimeout
	googleClient.Transport = rehttp.NewTransport(
		googleClient.Transport, // need to wrap DefaultClient transport
		rehttp.RetryAll(
			rehttp.RetryMaxRetries(*gcpMaxRetries),
			rehttp.RetryStatuses(*gcpRetryStatuses...)), // Cloud support suggests retrying on 503 errors
		rehttp.ExpJitterDelay(*gcpBackoffJitterBase, *gcpMaxBackoffDuration), // Set timeout to <10s as that is prom default timeout
	)

	computeService, err := compute.NewService(ctx, option.WithHTTPClient(googleClient))
	if err != nil {
		log.Fatalf("Unable to create service: %v", err)
	}

	cloudresourcemanagerService, err := cloudresourcemanager.NewService(ctx, option.WithHTTPClient(googleClient))
	if err != nil {
		log.Fatalf("Unable to create cloudresourcemanager service: %v", err)
	}

	serviceusageService, err := serviceusage.NewService(ctx, option.WithHTTPClient(googleClient))
	if err != nil {
		log.Fatalf("Unable to create serviceusage service: %v", err)
	}


	return &Exporter{
		computeService: computeService,
		resourceManagerService: cloudresourcemanagerService,
		serviceusageService: serviceusageService,
		project: project,
		folder: folder,
	}, nil
}

func GetProjectIdFromMetadata() (string, error) {
	client := metadata.NewClient(&http.Client{})

	project_id, err := client.ProjectID()
	if err != nil {
		return "", err
	}

	return project_id, nil
}

func main() {

	var (
		listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9592").String()
		metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
		basePath      = kingpin.Flag("test.base-path", "Change the default googleapis URL (for testing purposes only).").Default("").String()
	)

	//log.Flags(kingpin.CommandLine)
	kingpin.Version(version.Print("gcp_quota_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	log.Printf("Starting gcp_quota_exporter", version.Info())
	log.Printf("Build context", version.BuildContext())
	if *gcpParenfFolderID == "" {
		if *gcpProjectID == "" {
			credentialsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")

			if credentialsFile != "" {
				c, err := ioutil.ReadFile(credentialsFile)
				if err != nil {
					log.Fatalf("Unable to read %s: %v", credentialsFile, err)
				}

				projectId := gjson.GetBytes(c, "project_id")

				if projectId.String() == "" {
					log.Fatalf("Could not retrieve Project ID from %s", credentialsFile)
				}

				*gcpProjectID = projectId.String()
			} else {
				project_id, err := GetProjectIdFromMetadata()
				if err != nil {
					log.Fatal(err)
				}

				*gcpProjectID = project_id
			}
		}

		if *gcpProjectID == "" {
			log.Fatal("GCP Project ID cannot be empty")
		}
	}
	// Detect Project ID


	exporter, err := NewExporter(*gcpProjectID, *gcpParenfFolderID)
	if err != nil {
		log.Fatal(err)
	}

	if *basePath != "" {
		exporter.computeService.BasePath = *basePath
		exporter.serviceusageService.BasePath = *basePath
		exporter.resourceManagerService.BasePath = *basePath
	}
	go exporter.scrapeRoutine()

	prometheus.MustRegister(exporter)
	prometheus.MustRegister(version.NewCollector("gcp_quota_exporter"))

	log.Printf("Google Project:", *gcpProjectID)
	log.Printf("Google Folder:", *gcpParenfFolderID)
	log.Printf("Listening on", *listenAddress)
	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>GCP Quota Exporter</title></head>
             <body>
             <h1>GCP Quota Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})
	log.Fatal(http.ListenAndServe(*listenAddress, nil))

}
