participant TaskCRD
participant TaskOperator
participant CronJob
participant Job
participant Pod

TaskCRD -> TaskOperator: Watches
TaskOperator -> CronJob: Create CronJob
CronJob -> Job: Create Job

CronJob -> TaskOperator: Watches
TaskOperator -> TaskCRD: Updates Task Status

Job -> TaskOperator: Fetch Job Status
TaskOperator -> TaskCRD: Updates Task Status

Job -> Pod: Creates Pod
Pod -> Pod: Execute commands

Prometheus -> TaskOperator: Scrapes Metrics

Job -> Job: Deletes Completed Job