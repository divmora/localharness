import { useState } from 'react';
import { Square, Check } from 'lucide-react';
import { StepUpdate_State } from '../gen/localharness/v1/localharness_pb';

interface QuestionFormProps {
  action: any;
  state: StepUpdate_State;
  onSubmit: (answers: any[], skipped: boolean) => void;
}

export function QuestionForm({ action, state, onSubmit }: QuestionFormProps) {
  const [answers, setAnswers] = useState<any[]>(
    action.questions?.map(() => ({ selectedIndices: [], selectedOptions: [], text: '' })) || []
  );

  const [submitted, setSubmitted] = useState(false);

  const isDone = state === StepUpdate_State.DONE || submitted;

  if (isDone) {
    if (action.skipped) return <div className="text-text-tertiary italic text-xs mt-2">Skipped question.</div>;
    return (
      <div className="mt-2 flex flex-col gap-2">
        {action.questions?.map((q: any, i: number) => (
          <div key={i} className="bg-bg-tertiary p-2 rounded border border-border-primary">
            <div className="font-semibold text-xs mb-1">{q.question}</div>
            <div className="text-xs text-blue-400">
              {answers?.[i]?.selectedOptions?.length > 0 
                ? answers[i].selectedOptions.join(", ") 
                : answers?.[i]?.text || action.answers?.[i]?.text || "No answer"}
            </div>
          </div>
        ))}
        {submitted && <div className="text-[11px] text-text-secondary italic mt-1 text-right opacity-70">Response submitted</div>}
      </div>
    );
  }

  const handleToggle = (qIdx: number, optIdx: number, optText: string, isMulti: boolean) => {
    setAnswers(prev => {
      const next = [...prev];
      const cur = { ...next[qIdx] };
      if (isMulti) {
        if (cur.selectedIndices.includes(optIdx)) {
          cur.selectedIndices = cur.selectedIndices.filter((idx: number) => idx !== optIdx);
          cur.selectedOptions = cur.selectedOptions.filter((opt: string) => opt !== optText);
        } else {
          cur.selectedIndices = [...cur.selectedIndices, optIdx];
          cur.selectedOptions = [...cur.selectedOptions, optText];
        }
      } else {
        cur.selectedIndices = [optIdx];
        cur.selectedOptions = [optText];
      }
      next[qIdx] = cur;
      return next;
    });
  };

  const handleChangeText = (qIdx: number, text: string) => {
    setAnswers(prev => {
      const next = [...prev];
      next[qIdx] = { ...next[qIdx], text };
      return next;
    });
  };

  const handleConfirm = () => {
    onSubmit(answers, false);
    setSubmitted(true);
  };

  const handleSkip = () => {
    onSubmit([], true);
    setSubmitted(true);
  };

  return (
    <div className="mt-3 border border-border-highlight rounded-xl overflow-hidden shadow-sm max-w-md">
      <div className="flex items-center gap-2 px-3 py-2 text-xs text-text-primary bg-bg-tertiary border-b border-border-primary font-medium">
        <Square size={12} className="text-blue-500" />
        Agent Question
      </div>
      <div className="p-4 flex flex-col gap-4 text-xs">
        {action.questions?.map((q: any, qIdx: number) => (
          <div key={qIdx} className="flex flex-col gap-2">
            <div className="font-semibold text-text-primary">{q.question}</div>
            {q.options && q.options.length > 0 ? (
              <div className="flex flex-col gap-1.5 mt-1">
                {q.options.map((opt: string, optIdx: number) => {
                  const isSelected = answers[qIdx].selectedIndices.includes(optIdx);
                  return (
                    <label key={optIdx} className="flex items-center gap-2 cursor-pointer group" onClick={() => handleToggle(qIdx, optIdx, opt, q.isMultiSelect || false)}>
                      <div className={`w-4 h-4 rounded-sm border flex items-center justify-center transition-colors ${isSelected ? 'bg-blue-500 border-blue-500' : 'border-border-primary group-hover:border-blue-400 bg-bg-tertiary'}`}>
                        {isSelected && <Check size={10} className="text-white" />}
                      </div>
                      <span className={`${isSelected ? 'text-text-primary font-medium' : 'text-text-secondary group-hover:text-text-primary'} transition-colors`}>{opt}</span>
                    </label>
                  );
                })}
                {q.allowWriteIn && (
                  <input
                    type="text"
                    value={answers[qIdx].text}
                    onChange={(e) => handleChangeText(qIdx, e.target.value)}
                    placeholder="Other..."
                    className="mt-1 bg-bg-tertiary border border-border-primary rounded px-2.5 py-1.5 focus:outline-none focus:border-border-highlight text-text-primary"
                  />
                )}
              </div>
            ) : (
              <input
                type="text"
                value={answers[qIdx].text}
                onChange={(e) => handleChangeText(qIdx, e.target.value)}
                placeholder="Type your answer..."
                className="bg-bg-tertiary border border-border-primary rounded px-2.5 py-1.5 focus:outline-none focus:border-border-highlight text-text-primary w-full"
              />
            )}
          </div>
        ))}

        <div className="flex justify-end gap-2 mt-2 pt-3 border-t border-border-primary">
          <button 
            onClick={handleSkip}
            className="px-3 py-1.5 rounded-md text-text-secondary hover:text-text-primary hover:bg-bg-tertiary transition-colors font-medium"
          >
            Skip
          </button>
          <button 
            onClick={handleConfirm}
            className="px-4 py-1.5 rounded-md bg-blue-500 hover:bg-blue-600 text-white transition-colors font-medium shadow-sm"
          >
            Confirm
          </button>
        </div>
      </div>
    </div>
  );
}
