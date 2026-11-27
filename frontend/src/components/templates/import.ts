// 模板导入弹窗与模板页共享的错误状态（job 化后错误由父组件持有）。
import type { ApiFieldError } from "../../api/types";

export interface ImportErrorState {
  fieldErrors: ApiFieldError[];
  message: string;
}
