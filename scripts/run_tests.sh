#!/bin/bash

# Test Coverage Analysis Script
# This script runs tests for all packages and generates informational coverage reports.
# Cross-platform compatible (macOS and Linux)

set -e

# Configuration
COVERAGE_TARGET=85
COVERAGE_DIR="coverage"
COVERAGE_PROFILE="$COVERAGE_DIR/coverage.out"
COVERAGE_HTML="$COVERAGE_DIR/coverage.html"
COVERAGE_JSON="$COVERAGE_DIR/coverage.json"

# Parse command line arguments
VERBOSE=false
FAST_MODE=false
RACE_MODE=true
for arg in "$@"; do
    case $arg in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -f|--fast)
            FAST_MODE=true
            shift
            ;;
        --no-race)
            RACE_MODE=false
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -v, --verbose    Run tests with verbose output"
            echo "  -f, --fast       Run tests without coverage (faster, cached)"
            echo "      --no-race    Run coverage tests without the race detector"
            echo "  -h, --help       Show this help message"
            exit 0
            ;;
        *)
            # Unknown option
            ;;
    esac
done

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create coverage directory
mkdir -p "$COVERAGE_DIR"

if [ "$FAST_MODE" = true ]; then
    echo -e "${BLUE}===========================================${NC}"
    echo -e "${BLUE}            Fast Test Execution           ${NC}"
    echo -e "${BLUE}         (No Coverage Analysis)           ${NC}"
    echo -e "${BLUE}===========================================${NC}"
else
    echo -e "${BLUE}===========================================${NC}"
    if [ "$RACE_MODE" = true ]; then
        echo -e "${BLUE}      Go Test Coverage & Race Analysis    ${NC}"
    else
        echo -e "${BLUE}           Go Test Coverage Only          ${NC}"
    fi
    echo -e "${BLUE}===========================================${NC}"
fi
echo

# Function to generate HTML coverage report
generate_html_report() {
    if [ -f "$COVERAGE_PROFILE" ]; then
        echo -e "${BLUE}Generating HTML coverage report...${NC}"
        go tool cover -html="$COVERAGE_PROFILE" -o "$COVERAGE_HTML"
        echo -e "${GREEN}HTML report generated: $COVERAGE_HTML${NC}"
    fi
}

# Function to generate JSON coverage report
generate_json_report() {
    if [ -f "$COVERAGE_PROFILE" ]; then
        echo -e "${BLUE}Generating JSON coverage report...${NC}"
        go tool cover -func="$COVERAGE_PROFILE" | awk '
        BEGIN {
            print "{"
            print "  \"packages\": ["
            first = 1
        }
        /^total:/ {
            total_coverage = $3
            gsub(/%/, "", total_coverage)
            next
        }
        !/^total:/ && NF >= 3 {
            file = $1
            coverage = $3
            gsub(/%/, "", coverage)
            
            if (!first) print ","
            printf "    {\n"
            printf "      \"file\": \"%s\",\n", file
            printf "      \"coverage\": %.1f\n", coverage
            printf "    }"
            first = 0
        }
        END {
            print ""
            print "  ],"
            printf "  \"total_coverage\": %.1f\n", total_coverage
            print "}"
        }' > "$COVERAGE_JSON"
        echo -e "${GREEN}JSON report generated: $COVERAGE_JSON${NC}"
    fi
}

# Function to generate coverage badge
generate_coverage_badge() {
    if [ -f "$COVERAGE_PROFILE" ]; then
        echo -e "${BLUE}Generating coverage badge...${NC}"
        
        # Use go-test-coverage tool to generate badge
        if command -v go-test-coverage &> /dev/null; then
            local badge_file="$COVERAGE_DIR/coverage-badge.svg"
            # Allow go-test-coverage to fail (it exits with non-zero when coverage is below threshold)
            go-test-coverage --config=.testcoverage.yml --badge-file-name="$badge_file" >/dev/null 2>&1 || true
            echo -e "${GREEN}Coverage badge generated: $badge_file${NC}"
        else
            echo -e "${YELLOW}go-test-coverage not found. Install with: go install github.com/vladopajic/go-test-coverage/v2@latest${NC}"
        fi
    fi
}

# Function to generate coverage summary
generate_coverage_summary() {
    if [ -f "$COVERAGE_PROFILE" ]; then
        echo -e "${BLUE}===========================================${NC}"
        echo -e "${BLUE}          Coverage Summary                 ${NC}"
        echo -e "${BLUE}===========================================${NC}"
        
        # Overall coverage
        local total_coverage=$(go tool cover -func="$COVERAGE_PROFILE" | grep "total:" | awk '{print $3}')
        echo -e "${BLUE}Overall Coverage: ${total_coverage}${NC}"
        
        # Per-file coverage
        echo -e "${BLUE}Per-file Coverage (target: ${COVERAGE_TARGET}%):${NC}"
        go tool cover -func="$COVERAGE_PROFILE" | grep -v "total:" | while read -r line; do
            local file=$(echo "$line" | awk '{print $1}')
            local coverage=$(echo "$line" | awk '{print $3}')
            local coverage_num=$(echo "$coverage" | sed 's/%//')
            
            if (( $(echo "$coverage_num" | cut -d. -f1) >= $COVERAGE_TARGET )); then
                echo -e "${GREEN}  OK:  $file: $coverage${NC}"
            else
                echo -e "${YELLOW}  LOW: $file: $coverage${NC}"
            fi
        done
        
        echo
        echo -e "${BLUE}Reports generated:${NC}"
        echo -e "  HTML Report: $COVERAGE_HTML"
        echo -e "  JSON Report: $COVERAGE_JSON"
        echo -e "  Coverage Badge: $COVERAGE_DIR/coverage-badge.svg"
        echo -e "  Coverage Profile: $COVERAGE_PROFILE"
    fi
}

# Function to check dependencies (no longer needed with native bash arithmetic)
check_dependencies() {
    # All calculations now use native bash arithmetic - no external dependencies needed
    return 0
}

# Main execution
main() {
    check_dependencies
    
    # Clean up previous coverage data
    rm -f "$COVERAGE_DIR"/*.out "$COVERAGE_DIR"/*.html "$COVERAGE_DIR"/*.json "$COVERAGE_DIR"/profiles.list
    
    # Get all packages, excluding cmd packages (entry points). Run them as a
    # single go test invocation so Go can schedule packages in parallel.
    packages=$(go list ./... | grep -v '/cmd/')

    local test_args=()
    if [ "$VERBOSE" = true ]; then
        test_args+=("-v")
    fi

    if [ "$FAST_MODE" = true ]; then
        echo -e "${BLUE}Running fast tests across all packages...${NC}"
        if ! TESTING_MODE=true go test "${test_args[@]}" $packages; then
            echo -e "${RED}FAIL: Tests failed${NC}"
            exit 1
        fi
    else
        local coverage_args=("-coverprofile=$COVERAGE_PROFILE")
        if [ "$RACE_MODE" = true ]; then
            echo -e "${BLUE}Running race-enabled coverage tests across all packages...${NC}"
            coverage_args=("-race" "-coverprofile=$COVERAGE_PROFILE" "-covermode=atomic")
        else
            echo -e "${BLUE}Running coverage tests across all packages...${NC}"
        fi

        if ! TESTING_MODE=true go test "${coverage_args[@]}" "${test_args[@]}" $packages; then
            echo -e "${RED}FAIL: Tests failed${NC}"
            exit 1
        fi
    fi

    # Generate reports only in coverage mode.
    if [ "$FAST_MODE" = false ]; then
        generate_html_report
        generate_json_report
        generate_coverage_badge
        generate_coverage_summary
    fi
    
    # Final summary
    echo -e "${BLUE}===========================================${NC}"
    echo -e "${BLUE}          Final Results                    ${NC}"
    echo -e "${BLUE}===========================================${NC}"
    
    local total_packages
    total_packages=$(echo "$packages" | wc -w | tr -d ' ')
    echo -e "${BLUE}Total packages: $total_packages${NC}"
    
    if [ "$FAST_MODE" = true ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo -e "${GREEN}OK: All tests passed! Coverage reports generated for visibility.${NC}"
        exit 0
    fi
}

# Function to clean up generated test files
cleanup_test_files() {
    echo -e "${BLUE}Cleaning up generated test files...${NC}"
    
    # Remove .generated files from golden directories
    find . -name "*.generated" -type f -delete 2>/dev/null || true
    
    # Remove test binaries
    find . -name "*-golden-test" -type f -delete 2>/dev/null || true
    find . -name "*-regression-test" -type f -delete 2>/dev/null || true
    find . -name "*-test" -type f -delete 2>/dev/null || true
    
    echo -e "${GREEN}Test cleanup completed${NC}"
}

# Set up cleanup to run on script exit
trap cleanup_test_files EXIT

# Run main function
main "$@"
