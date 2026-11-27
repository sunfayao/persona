// The messages the to-do strip prepares for the owner to edit and send: their exact words, from the to-do's facts.
import { describe, expect, it } from "vitest";
import { continueMessage, nextCriterionMessage, reportMessage, supervisionMessage, type SteerTodo } from "./todo-steer";

const facts: SteerTodo = {
  id: "todo_1",
  title: 'Fix the "landscape"\nwidget',
  stage: "develop",
  criteria: [
    { n: 1, text: "The fix is merged", mark: "met" },
    { n: 2, text: "A snapshot test covers landscape", mark: "missing" },
    { n: 3, text: "The release notes say so", mark: "open" },
  ],
};
const bare: SteerTodo = { id: "todo_2", title: "Tidy the docs", criteria: [] };

describe("to-do steering messages", () => {
  it("Continue lists the criteria with their marks and names the first one not met", () => {
    expect(continueMessage(facts)).toBe(
      [
        "To-do \"Fix the 'landscape' widget\" (todo_1), stage: develop.",
        "Done when:",
        "1. [met] The fix is merged",
        "2. [not met] A snapshot test covers landscape",
        "3. [open] The release notes say so",
        "Say in one line per criterion where you stand against it, then continue with the first one not yet met: criterion 2.",
      ].join("\n"),
    );
    const allMet = { ...facts, criteria: facts.criteria.map((c) => ({ ...c, mark: "met" as const })) };
    expect(continueMessage(allMet).split("\n").at(-1)).toBe("Say in one line per criterion what shows it is met. If one is not, continue with it.");
    expect(continueMessage(bare)).toBe("To-do \"Tidy the docs\" (todo_2).\nSay in one line where you stand against this to-do, then continue with its next step.");
  });

  it("Report asks for one status line per criterion and nothing else", () => {
    expect(reportMessage(facts)).toBe(
      [
        "To-do \"Fix the 'landscape' widget\" (todo_1), stage: develop.",
        "Done when:",
        "1. The fix is merged",
        "2. A snapshot test covers landscape",
        "3. The release notes say so",
        "For each criterion, report only its status in one short line: done, not done, or blocked and why. Nothing else.",
      ].join("\n"),
    );
    expect(reportMessage(bare)).toBe("To-do \"Tidy the docs\" (todo_2).\nReport your status on this to-do in a few short lines: done, not done, or blocked and why. Nothing else.");
  });

  it("Do this next names one criterion by its number and words", () => {
    expect(nextCriterionMessage(facts, facts.criteria[2])).toBe(
      "To-do \"Fix the 'landscape' widget\" (todo_1), stage: develop.\nWork next on criterion 3: \"The release notes say so\". Say when it is met and what shows it.",
    );
  });

  it("the main conversation's draft names the to-do and this agent", () => {
    expect(supervisionMessage(bare, { title: "Docs pass", machine_id: "mac", session_id: "s1" })).toBe(
      "Please take over supervising the to-do \"Tidy the docs\" (todo_2). The agent \"Docs pass\" (session s1 on computer mac) works on it. Check where it stands against its criteria and guide the agent until it is done.",
    );
  });
});
