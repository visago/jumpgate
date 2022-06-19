#!/usr/bin/env bash
TIMEOUT=30
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
#echo "strace -f bin/jumpgate --source ${JUMPGATELISTEN} --target ${METRICSLISTEN} --metrics-listen ${METRICSLISTEN} --pidfile ${PIDFILE}"
#strace -f 
bin/jumpgate --source ${JUMPGATELISTEN} --target ${METRICSLISTEN} --metrics-listen ${METRICSLISTEN} --pidfile ${PIDFILE} &> ${TESTLOGFILE} &

while [ ! -f ${PIDFILE} ]; do
  if [ "$TIMEOUT" == 0 ]; then
    echo "### ERROR PIDFILE - Timeout while waiting for the file ${PIDFILE} to exist"
    exit 1
  fi
  sleep 1
  ((TIMEOUT--))
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

### Ensures concurrent connection
if ab -n 100 -c 10 http://${JUMPGATELISTEN}/metrics; then
  echo "### OKAY AB - Multiple connection count is supported"
else
  echo "### ERROR AB - Failed to handle multiple connections"
  exit 1
fi

### Test volume
#curl -s "http://${METRICSLISTEN}/metrics/reset"
if true; then
  PERCENT=100
  MODE=normal
  MAX=100
  PASS=0
  TARGET=${MAX}
  for i in $(seq ${MAX}); do
    if curl -s --fail --max-time 3 -o /dev/null http://${JUMPGATELISTEN}/metrics 2>/dev/null; then
      PASS=$(( PASS + 1 ))
    fi
  done
  VARIANT=$(( ( TARGET - PASS ) * 100 / MAX ))
  curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate_connections"
  if [[ "${VARIANT#-}" -lt "10" ]]; then
    echo "### OKAY ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
  else
    echo "### ERROR ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
    exit 1
  fi
fi

### Test mangling - lag
if true; then
  for PERCENT in 25 75; do
    MODE=lag
    MAX=100
    LAG=5
    PASS=0
    TARGET=$(( MAX * ( 100 - PERCENT ) / 100 )) #TARGET
    curl -s "http://${METRICSLISTEN}/mangle?mode=${MODE}&lag=${LAG}&percent=${PERCENT}"
    curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate"
    for i in $(seq ${MAX}); do
      if curl -s --fail --max-time 2 -o /dev/null http://${JUMPGATELISTEN}/metrics 2>/dev/null; then
        PASS=$(( PASS + 1 ))
      fi
    done
    VARIANT=$(( ( TARGET - PASS ) * 100 / MAX ))
    curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate"
    if [[ "${VARIANT#-}" -lt "10" ]]; then
      echo "### OKAY ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
    else
      echo "### ERROR ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
      exit 1
    fi
  done
fi

### Test mangling - close
for PERCENT in 10 25 50 75 90; do
  #curl -s "http://${METRICSLISTEN}/metrics/reset"
  MODE=close
  MAX=100
  PASS=0
  TARGET=$(( MAX * ( 100 - PERCENT ) / 100 )) #TARGET
  curl -s "http://${METRICSLISTEN}/mangle?mode=${MODE}&percent=${PERCENT}"
  for i in $(seq ${MAX}); do
    if curl -s --fail --max-time 3 -o /dev/null http://${JUMPGATELISTEN}/metrics 2>/dev/null; then
      PASS=$(( PASS + 1 ))
    fi
  done
  VARIANT=$(( ( TARGET - PASS ) * 100 / MAX ))
  curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate_connections"
  if [[ "${VARIANT#-}" -lt "15" ]]; then
    echo "### OKAY ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
  else
    echo "### ERROR ${PERCENT}${MODE} - Variant of ${VARIANT#-}% with ${PASS} out of ${MAX} times, with target of ${TARGET} (${PERCENT}%)"
    curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate"
    exit 1
  fi
done

#curl -s "http://${METRICSLISTEN}/metrics" | grep "^jumpgate"

### Check if there's any connections left
sleep 1
OUTPUT=$(curl -s "http://${METRICSLISTEN}/mangle/dump")
if [[ -n "${OUTPUT}" ]]; then
  echo "### ERROR REMAINING - Found output of ${OUTPUT} in dump. Means we got connections unclosed"
  exit 1
else
  echo "### OK REMAINING - No connections remaining. Good !"
fi

### Reset connection
TOUCH=nc.touch
$(rm -f ${TOUCH}; nc 127.0.0.1 9000; touch ${TOUCH}) &
sleep 1
if [[ -f nc.touch ]]; then
  echo "### ERROR RESETCHECK - touch file ${TOUCH} exists. Maybe jumpgate is down"
fi
curl -s "http://${METRICSLISTEN}/mangle/reset"
sleep 1
if [[ -f nc.touch ]]; then
  echo "### OK RESETCHECK - touch file ${TOUCH} now exists. Means connection terminated"
else 
  echo "### ERROR RESETCHECK - touch file ${TOUCH} does not exist. Jumpgate failed to reset"
fi
rm -f ${TOUCH}


### Send SIGTERM to quit process
if kill -TERM ${PID}; then
  echo "### OKAY SIGTERM - PID ${PID} sent SIGTERM"
else
  echo "### ERROR SIGTERM - Failed to kill PID ${PID}"
  exit 1
fi

### Wait for process to quit
while kill -0 ${PID} &>/dev/null ; do
  if [[ "$TIMEOUT" -le "0" ]]; then
    echo "### ERROR PIDTERM - Timeout while waiting for the process PID ${PID} to quit"
    exit 1
  fi
  sleep 1
  ((TIMEOUT--))
done
echo "### OKAY PIDTERM - PID ${PID} quit"

### Check if the PIDFILE was cleaned up
if [[ -f "${PIDFILE}" ]]; then
  echo "### ERROR CLEANUPPIDFILE - PIDFILE ${PIDFILE} still exists even when we terminated process"
  exit 1
fi
echo "### OKAY CLEANUPPIDFILE"
echo "### OKAY ALLTEST"


