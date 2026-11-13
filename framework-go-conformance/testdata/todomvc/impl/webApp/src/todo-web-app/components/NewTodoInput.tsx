import { useEffect, useRef, useState, type JSX } from 'react';

export function NewTodoInput({
  disabled,
  onAdd,
}: {
  disabled: boolean;
  onAdd: (wording: string) => Promise<boolean>;
}): JSX.Element {
  const [text, setText] = useState('');
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!disabled) ref.current?.focus();
  }, [disabled]);
  return (
    <input
      aria-label="What needs to be done?"
      className="new-todo"
      data-testid="new-todo-input"
      disabled={disabled}
      placeholder="What needs to be done?"
      ref={ref}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
      }}
      onKeyDown={(e) => {
        if (e.key !== 'Enter') return;
        void onAdd(text).then((ok) => {
          if (ok) setText('');
        });
      }}
    />
  );
}
