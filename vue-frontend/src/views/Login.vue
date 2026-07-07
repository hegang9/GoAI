<template>
  <GradientBackground class="login-root">
    <AuthCard title="登录">
      <el-form
        ref="loginFormRef"
        :model="loginForm"
        :rules="loginRules"
        label-width="80px"
      >
        <el-form-item label="邮箱" prop="email">
          <el-input
            v-model="loginForm.email"
            placeholder="请输入邮箱"
            type="email"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="loginForm.password"
            placeholder="请输入密码"
            type="password"
            show-password
          />
        </el-form-item>
        <el-form-item>
          <el-button
            class="auth-primary-action"
            type="primary"
            :loading="loading"
            @click="handleLogin"
            style="width: 100%"
          >
            登录
          </el-button>
        </el-form-item>
        <el-form-item>
          <el-button
            class="auth-link-action"
            type="text"
            @click="$router.push('/register')"
            style="width: 100%"
          >
            还没有账号？去注册
          </el-button>
        </el-form-item>
      </el-form>
    </AuthCard>
  </GradientBackground>
</template>

<script>
import {ref} from "vue";
import {useRouter} from "vue-router";
import {ElMessage} from "element-plus";
import api from "../utils/api";
import GradientBackground from "../components/GradientBackground.vue";
import AuthCard from "../components/auth/AuthCard.vue";

export default {
  name: "LoginView",
  components: {GradientBackground, AuthCard},
  setup() {
    const router = useRouter();
    const loginFormRef = ref();
    const loading = ref(false);
    const loginForm = ref({email: "", password: ""});

    const loginRules = {
      email: [
        {required: true, message: "请输入邮箱", trigger: "blur"},
        {type: "email", message: "请输入正确的邮箱格式", trigger: "blur"},
      ],
      password: [
        {required: true, message: "请输入密码", trigger: "blur"},
        {min: 6, message: "密码长度不能少于6位", trigger: "blur"},
      ],
    };

    const handleLogin = async () => {
      try {
        await loginFormRef.value.validate();
        loading.value = true;
        const response = await api.post("/user/login", {
          email: loginForm.value.email,
          password: loginForm.value.password,
        });
        if (response.data.status_code === 1000) {
          localStorage.setItem("token", response.data.token);
          ElMessage.success("登录成功");
          router.push("/menu");
        } else {
          ElMessage.error(response.data.status_msg || "登录失败");
        }
      } catch (error) {
        console.error("Login error:", error);
        ElMessage.error("登录失败，请重试");
      } finally {
        loading.value = false;
      }
    };

    return {loginFormRef, loading, loginForm, loginRules, handleLogin};
  },
};
</script>

<style scoped>
.login-root {
  display: flex;
  justify-content: center;
  align-items: center;
}
</style>
