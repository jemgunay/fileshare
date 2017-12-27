$(document).ready(function() {
    // login page
    if (window.location.pathname === "/login") {
        setButtonProcessing($("#login-btn"), false);

        // login form submit
        $("#login-form").submit(function(e) {
            e.preventDefault();

            var data = $(this).serialize();

            setButtonProcessing($("#login-btn"), true);

            performRequest(hostname + "/login", "post", data, function(result) {
                result = result.trim();

                if (result === "unauthorised") {
                    setAlertWindow("warning", "Incorrect email address or password.", "#error-window");
                    $("#password-input").val("");
                    setButtonProcessing($("#login-btn"), false);
                }
                else if (result === "error") {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                    $("#password-input").val("");
                    setButtonProcessing($("#login-btn"), false);
                }
                else {
                    window.location = "/";
                }
            });
        });
    }

    // register page
    else if (window.location.pathname === "/register") {
        setButtonProcessing($("#register-btn"), false);

        $("#register-form").submit(function (e) {
            e.preventDefault();

            var data = $(this).serialize();

            setButtonProcessing($("#register-btn"), true);

            performRequest(hostname + "/register/email", "post", data, function(result) {
                result = result.trim();

                if (result === "success") {
                    setAlertWindow("success", "Access request submitted for '" + $("#email-input").val() + "'! Check for an email once the request has been accepted by an administrator.", "#error-window");
                    $("#email-input").val("");
                    setButtonProcessing($("#register-btn"), false);
                }
                else {
                    setAlertWindow("danger", "A server error occurred.", "#error-window");
                    $("#password-input").val("");
                    setButtonProcessing($("#register-btn"), false);
                }
            });
        });
    }
});