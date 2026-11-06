## Contributing

We welcome contributions of all kinds to improve this project! Here's how you can get involved:

1. **Fork the Repository**: Start by forking the repository to your GitHub account.

2. **Clone Your Fork**: Clone the forked repository to your local machine:

   ```bash
   git clone git@github.com:YOUR_GITHUB_USERNAME/ledger-to-chain-reconciler.git
   cd ledger-to-chain-reconciler
   ```

3. **Create a Branch**: Create a new branch for your feature or bug fix:

   ```bash
   git checkout -b feature-name
   ```

4. **Make Changes**: Make your changes to the codebase. Be sure to follow existing code style and conventions.

5. **Commit Changes**: Commit your changes with a clear and descriptive message:

   ```bash
   git commit -m "Add detailed description of the changes"
   ```

6. **Push Changes**: Push your changes to your fork:

   ```bash
   git push origin feature-name
   ```

7. **Submit a Pull Request**: Open a pull request from your branch to the main repository's branch. Include a detailed description of your changes and the problem they solve.

8. **Review and Feedback**: Participate in the review process. Make any requested changes, and your contribution will be merged!

### Guidelines

- Ensure your code follows the existing style and conventions.
- Include tests for any new functionality or bug fixes.
- Write clear, concise commit messages and pull request descriptions.

### Issues

Feel free to report bugs or suggest enhancements by creating an issue. Be sure to include detailed information about the problem and, if possible, steps to reproduce it.

### Local verification

Use Go 1.27.1+ and run `make check` and `make race`. Changes to receipt processing,
matching, source isolation, coverage or history need regression tests. Install
Foundry (`anvil`, `forge`, `cast`, tested v1.4.4) and Python 3.9+ before running
`make integration` and `make demo`. Tests use only localhost, with no production
credentials. Keep fixture broadcasting out of production Go packages. Document
any changes to finding codes, severity, accounting eligibility or exit behavior.
