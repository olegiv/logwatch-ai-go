Run tests with coverage report and open the HTML visualization.

Execute the following steps:

1. Run tests with coverage:
   ```bash
   make test-coverage
   ```

2. This will:
   - Run all tests with coverage tracking
   - Generate coverage.out file
   - Create coverage.html with visualization

3. Report coverage statistics:
   - Overall coverage percentage
   - Per-package coverage breakdown
   - Identify packages with low coverage (<60%)

4. Suggest areas for improvement:
   - Which packages need more tests
   - What types of tests are missing

5. Provide the path to the HTML report:
   - coverage.html can be opened in a browser for detailed visualization

Note: The HTML file will show line-by-line coverage with green (covered) and red (uncovered) highlighting.
