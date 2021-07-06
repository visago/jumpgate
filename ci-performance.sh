#!/usr/bin/env bash
TIMEOUT=60
PIDFILE="/tmp/jumpgate.pid"
TESTLOGFILE="test.log"
METRICSLISTEN="127.0.0.1:9878"
JUMPGATELISTEN="0.0.0.0:5002" 
JUMPGATETARGET="127.0.0.1:5001" # Assumes a local iperf instance
VERSION=$(git describe --tags --always --dirty="-dev")
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
nohup bin/jumpgate --source "${JUMPGATELISTEN}" --target "${JUMPGATETARGET}" --metrics-listen "${METRICSLISTEN}" --pidfile ${PIDFILE} &> ${TESTLOGFILE} &
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

echo "# Jumpgate Version ${VERSION} Performance tests" > PERFORMANCE-${VERSION}.MD

echo -e "\n## IPERF Native performance, single connection" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD
echo "# iperf -c 127.0.0.1 --port 5001 --parallel 1" >> PERFORMANCE-${VERSION}.MD
iperf -c 127.0.0.1 --port 5001 --parallel 1  &>> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD

echo -e "\n## IPERF Native performance, 10 connection" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD
echo "# iperf -c 127.0.0.1 --port 5001 --parallel 10" >> PERFORMANCE-${VERSION}.MD
iperf -c 127.0.0.1 --port 5001 --parallel 1  &>> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD

echo -e "\n## IPERF Jumpgate performance, single connection" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD
echo "# iperf -c 127.0.0.1 --port 5002 --parallel 1" >> PERFORMANCE-${VERSION}.MD
iperf -c 127.0.0.1 --port 5002 --parallel 1  &>> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD

echo -e "\n## IPERF Jumpgate performance, 10 connection" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD
echo "# iperf -c 127.0.0.1 --port 5002 --parallel 10" >> PERFORMANCE-${VERSION}.MD
iperf -c 127.0.0.1 --port 5002 --parallel 10  &>> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD

echo -e "\n## Jumpgate metrics" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD
curl -s http://${METRICSLISTEN}/metrics |& grep -i "^jumpgate" >> PERFORMANCE-${VERSION}.MD
echo -e '```' >> PERFORMANCE-${VERSION}.MD

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


