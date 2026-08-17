import React, { useState } from 'react';
import { invoke } from '@tauri-apps/api/core';

interface DepositModalProps {
  officeId: string;
  currentBalance: number;
  onClose: () => void;
  onDepositComplete: (newBalance: number) => void;
}

export const DepositModal: React.FC<DepositModalProps> = ({ officeId, currentBalance, onClose, onDepositComplete }) => {
  const [customAmount, setCustomAmount] = useState<string>('');
  const [isDepositing, setIsDepositing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const predefinedAmounts = [1000, 5000, 10000];

  const handleDeposit = async (amount: number) => {
    if (amount <= 0 || isNaN(amount)) {
      setError("Please enter a valid positive amount.");
      return;
    }
    setError(null);
    setIsDepositing(true);
    try {
      const newBalance = await invoke<number>('add_wallet_balance', { officeId, amount });
      onDepositComplete(newBalance);
      onClose();
    } catch (err: any) {
      setError(err.toString());
    } finally {
      setIsDepositing(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm">
      <div className="bg-bg-primary border border-border-primary rounded-xl shadow-2xl p-6 max-w-sm w-full relative overflow-hidden">
        {/* Glassmorphic decorative elements */}
        <div className="absolute top-0 right-0 w-32 h-32 bg-blue-500/10 rounded-full blur-3xl -mr-16 -mt-16 pointer-events-none"></div>
        
        <h2 className="text-xl font-bold text-text-primary mb-1">Add Divcoins</h2>
        <p className="text-sm text-text-secondary mb-6">Fund this office's budget to run agent operations.</p>
        
        <div className="flex justify-between items-end mb-6 bg-bg-secondary p-4 rounded-lg border border-border-primary">
          <span className="text-sm text-text-secondary">Current Balance</span>
          <span className="text-2xl font-bold text-text-primary">{currentBalance.toFixed(0)} <span className="text-sm text-text-tertiary">DC</span></span>
        </div>

        <div className="mb-4">
          <label className="block text-xs font-semibold text-text-secondary uppercase mb-2">Quick Add</label>
          <div className="grid grid-cols-3 gap-2">
            {predefinedAmounts.map((amt) => (
              <button
                key={amt}
                onClick={() => handleDeposit(amt)}
                disabled={isDepositing}
                className="bg-bg-secondary hover:bg-bg-tertiary text-text-primary py-2 rounded-lg border border-border-primary transition-colors text-sm font-semibold disabled:opacity-50"
              >
                +{amt}
              </button>
            ))}
          </div>
        </div>

        <div className="mb-6">
          <label className="block text-xs font-semibold text-text-secondary uppercase mb-2">Custom Amount</label>
          <div className="flex gap-2">
            <div className="relative flex-1">
              <span className="absolute inset-y-0 left-0 pl-3 flex items-center text-text-tertiary font-bold">
                DC
              </span>
              <input
                type="number"
                value={customAmount}
                onChange={(e) => setCustomAmount(e.target.value)}
                placeholder="Enter amount..."
                className="w-full bg-bg-secondary border border-border-primary text-text-primary text-sm rounded-lg pl-9 pr-3 py-2 outline-none focus:border-blue-500 transition-colors"
                disabled={isDepositing}
              />
            </div>
            <button
              onClick={() => handleDeposit(parseFloat(customAmount))}
              disabled={isDepositing || !customAmount}
              className="bg-blue-600 hover:bg-blue-700 text-white font-semibold py-2 px-4 rounded-lg transition-colors disabled:opacity-50"
            >
              Add
            </button>
          </div>
        </div>

        {error && (
          <div className="mb-4 text-xs text-red-400 bg-red-400/10 p-2 rounded">
            {error}
          </div>
        )}

        <div className="flex justify-end">
          <button
            onClick={onClose}
            disabled={isDepositing}
            className="text-sm text-text-secondary hover:text-text-primary transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
};
