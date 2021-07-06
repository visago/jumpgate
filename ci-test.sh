#!/usr/bin/env bash
TIMEOUT=60
PIDFILE="/tmp/jumpgate.pid"
TESTLOGFILE="test.log"
METRICSLISTEN="127.0.0.1:9878"
JUMPGATELISTEN="127.0.0.1:9000"

set -o pipefail

### Clean up old files
if [[ -f "${PIDFILE}" ]]; then # Old file exists, kill and remove
  PID=$(cat ${PIDFILE})
  kill -9 ${PID}
  rm -f ${PIDFILE}
  echo "### Old PIDFILE ${PIDFILE} with PID of ${PID} exists. Killing process and removing the file"
fi

### Test build
if make; then
  echo "### OKAY BUILD"
else
  echo "### ERROR BUILD - Build failed"
  exit 1;
fi

### We do a jumpgate to the metrics listener port so we don't need to spin up a test server
nohup bin/jumpgate --source "${JUMPGATELISTEN}" --target "${METRICSLISTEN}" --metrics-listen "${METRICSLISTEN}" --pidfile ${PIDFILE} &> ${TESTLOGFILE} &
while [ ! -f ${PIDFILE} ]; do
  if [ "$timeout" == 0 ]; then
    echo "### ERROR PIDFILE - Timeout while waiting for the file ${PIDFILE} to exist"
    exit 1
  fi
  sleep 1
  ((timeout--))
done
echo "### OKAY PIDFILE - ${PIDFILE} CREATED"

### Test for process if it crashed
PID=$(cat ${PIDFILE})
if kill -0 ${PID} &>/dev/null ; then
  echo "### OKAY PIDPROCESS - PID ${PID} EXISTS"
else
  echo "### ERROR PIDPROCESS - PID ${PID} does not exist"
  exit 2
fi

### Test metrics port first
METRICSOUTPUT1=$(curl -s http://${METRICSLISTEN}/metrics |& grep -i "^jumpgate_build_info")
if [[ -n "${METRICSOUTPUT1}" ]]; then
  echo "### OKAY METRICS - ${METRICSOUTPUT1}"
else
  echo "### ERROR METRICS - No metrics output on ${METRICSLISTEN}"
  exit 1
fi

### Test jumpgate port first
METRICSOUTPUT2=$(curl -s http://${JUMPGATELISTEN}/metrics |& grep -i "^jumpgate_build_info")
if [[ -n "${METRICSOUTPUT2}" ]]; then
  echo "### OKAY JUMPMETRICS - ${METRICSOUTPUT2}"
else
  echo "### ERROR JUMPMETRICS - No metrics output on ${JUMPGATELISTEN}"
  exit 1
fi

### Ensure test outputs are the same
if [[ "${METRICSOUTPUT1}" == "${METRICSOUTPUT2}" ]]; then
  echo "### OKAY METRICSMATCH - Jumpgate and metrics port have same reply"
else
  echo "### ERROR METRICSMATCH - Output from jumpgate and metrics port is different. Bad relay !"
  exit 1
fi

### Send SIGTERM to quit process
if kill -TERM ${PID}; then
  echo "### OKAY SIGTERM - PID ${PID} sent SIGTERM"
else
  echo "### ERROR SIGTERM - Failed to kill PID ${PID}"
  exit 1
fi

### Wait for process to quit
while kill -0 ${PID} &>/dev/null ; do
  if [ "$timeout" == 0 ]; then
    echo "### ERROR PIDTERM - Timeout while waiting for the process PID ${PID} to quit"
    exit 1
  fi
  sleep 1
  ((timeout--))
done
echo "### OKAY PIDTERM - PID ${PID} quit"

### Check if the PIDFILE was cleaned up
if [[ -f "${PIDFILE}" ]]; then
  echo "### ERROR CLEANUPPIDFILE - PIDFILE ${PIDFILE} still exists even when we terminated process"
  exit 1
fi
echo "### OKAY CLEANUPPIDFILE"
echo "### OKAY ALLTEST"


