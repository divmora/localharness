#!/bin/bash
echo "Starting test run..." > scratch/test-results.txt
for dir in examples/*/; do
  if [ -f "$dir/main.go" ]; then
    echo "----------------------------------------" | tee -a scratch/test-results.txt
    echo "Running $dir" | tee -a scratch/test-results.txt
    
    # Run the example with a 120-second timeout and redirect stdin from /dev/null
    timeout 120s go run "./$dir" --prompt "Hello" < /dev/null >> scratch/test-results.txt 2>&1
    status=$?
    
    if [ $status -eq 124 ]; then
      echo "RESULT: TIMEOUT (expected for long-running/interactive agents)" | tee -a scratch/test-results.txt
    elif [ $status -eq 0 ]; then
      echo "RESULT: SUCCESS" | tee -a scratch/test-results.txt
    else
      echo "RESULT: FAILED (exit code $status)" | tee -a scratch/test-results.txt
    fi
  fi
done
echo "Finished all tests." | tee -a scratch/test-results.txt
