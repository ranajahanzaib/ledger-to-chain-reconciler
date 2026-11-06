// SPDX-License-Identifier: MIT
pragma solidity 0.8.30;

/// @notice Local testing fixture only. Unrestricted minting is intentional.
contract TestToken {
    string public constant name = "Fixture Dollar";
    string public constant symbol = "TESTUSD";
    uint8 public immutable decimals;
    mapping(address => uint256) public balanceOf;
    event Transfer(address indexed from, address indexed to, uint256 value);

    constructor(uint8 decimals_) { decimals = decimals_; }
    function mint(address to, uint256 amount) external {
        balanceOf[to] += amount;
        emit Transfer(address(0), to, amount);
    }
    function transfer(address to, uint256 amount) public returns (bool) {
        require(balanceOf[msg.sender] >= amount, "insufficient balance");
        balanceOf[msg.sender] -= amount;
        balanceOf[to] += amount;
        emit Transfer(msg.sender, to, amount);
        return true;
    }
    function multiTransfer(address[] calldata recipients, uint256[] calldata amounts) external {
        require(recipients.length == amounts.length, "length mismatch");
        for (uint256 i; i < recipients.length; ++i) transfer(recipients[i], amounts[i]);
    }
}
