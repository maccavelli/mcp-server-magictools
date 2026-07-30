sed -i 's/SetupGlobalLogger(levelVar, "text")/SetupGlobalLogger(nil, "test.log", "text", levelVar, false, false, os.Stdout, nil)/g' internal/logging/logging_test.go
sed -i 's/SetupGlobalLogger(levelVar, "json")/SetupGlobalLogger(nil, "test.log", "json", levelVar, false, false, os.Stdout, nil)/g' internal/logging/logging_test.go
